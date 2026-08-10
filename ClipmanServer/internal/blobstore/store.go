package blobstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
)

const (
	databaseFilename = "clipman-history.clipdb"
	metadataFilename = "clipman-server-metadata.json"
	touchInterval    = 5 * time.Minute
)

var ErrNotFound = errors.New("database not found")

type Options struct {
	Root                    string
	MaxDatabaseBytes        int64
	CreateBackupBeforeWrite bool
	BackupInterval          time.Duration
	BackupRetention         time.Duration
	MaxBackups              int
	Now                     func() time.Time
}

type Store struct {
	options Options
	locks   keyedLocks
}

type Info struct {
	Revision string
	Length   int64
	Modified time.Time
}

type Conditions struct {
	Match      string
	CreateOnly bool
}

type PutResult struct {
	Info      Info
	Identical bool
}

type ConflictError struct {
	Status   int
	Message  string
	Revision string
}

func (e *ConflictError) Error() string { return e.Message }

func New(options Options) (*Store, error) {
	if options.Root == "" {
		return nil, errors.New("blob store root is required")
	}
	if options.MaxDatabaseBytes <= 0 {
		return nil, errors.New("maximum database size must be positive")
	}
	if options.MaxBackups < 0 {
		return nil, errors.New("maximum backups cannot be negative")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Store{options: options}, nil
}

func ValidDatabaseID(id string) bool {
	if len(id) < 32 || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		character := id[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Head(id string) (Info, error) {
	release := s.locks.acquire(id)
	defer release()
	path := s.databasePath(id)
	info, err := fileInfo(path)
	if errors.Is(err, os.ErrNotExist) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	if err = s.touch(path, id, "head"); err != nil {
		return Info{}, err
	}
	return info, nil
}

func (s *Store) Get(ctx context.Context, id string, begin func(Info) error, destination io.Writer) (Info, int64, error) {
	release := s.locks.acquire(id)
	defer release()
	path := s.databasePath(id)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Info{}, 0, ErrNotFound
	}
	if err != nil {
		return Info{}, 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return Info{}, 0, err
	}
	info := infoFromStat(stat)
	if err = s.touch(path, id, "download"); err != nil {
		return Info{}, 0, err
	}
	if begin != nil {
		if err = begin(info); err != nil {
			return info, 0, err
		}
	}
	written, err := copyContext(ctx, destination, file)
	return info, written, err
}

func (s *Store) Put(ctx context.Context, id string, source io.Reader, length int64, conditions Conditions) (PutResult, error) {
	if length < 0 {
		return PutResult{}, errors.New("content length cannot be negative")
	}
	if length > s.options.MaxDatabaseBytes {
		return PutResult{}, fmt.Errorf("database exceeds the configured %d byte limit", s.options.MaxDatabaseBytes)
	}
	stagingDirectory := filepath.Join(s.options.Root, ".uploads")
	if err := os.MkdirAll(stagingDirectory, 0o700); err != nil {
		return PutResult{}, err
	}
	staging, err := os.CreateTemp(stagingDirectory, "database-*.upload.tmp")
	if err != nil {
		return PutResult{}, err
	}
	stagingPath := staging.Name()
	committed := false
	defer func() {
		_ = staging.Close()
		if !committed {
			_ = os.Remove(stagingPath)
		}
	}()
	if err = staging.Chmod(0o600); err != nil {
		return PutResult{}, err
	}
	written, err := copyExactly(ctx, staging, source, length)
	if err != nil {
		return PutResult{}, err
	}
	if written != length {
		return PutResult{}, io.ErrUnexpectedEOF
	}
	if err = staging.Sync(); err != nil {
		return PutResult{}, err
	}
	if err = staging.Close(); err != nil {
		return PutResult{}, err
	}

	release := s.locks.acquire(id)
	defer release()
	databasePath := s.databasePath(id)
	current, statErr := fileInfo(databasePath)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return PutResult{}, statErr
	}
	if conditions.CreateOnly && exists {
		return PutResult{}, &ConflictError{Status: 412, Message: "Database already exists", Revision: current.Revision}
	}
	if conditions.Match != "" && conditions.Match != current.Revision {
		return PutResult{}, &ConflictError{Status: 409, Message: "Database revision changed", Revision: current.Revision}
	}
	if exists {
		identical, compareErr := equalFiles(databasePath, stagingPath)
		if compareErr != nil {
			return PutResult{}, compareErr
		}
		if identical {
			if err = s.touch(databasePath, id, "head"); err != nil {
				return PutResult{}, err
			}
			return PutResult{Info: current, Identical: true}, nil
		}
		if s.options.CreateBackupBeforeWrite {
			if err = s.createBackup(databasePath); err != nil {
				return PutResult{}, err
			}
		}
	}
	if err = os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return PutResult{}, err
	}
	if err = os.Rename(stagingPath, databasePath); err != nil {
		return PutResult{}, err
	}
	committed = true
	if err = os.Chmod(databasePath, 0o600); err != nil {
		return PutResult{}, err
	}
	if err = s.touch(databasePath, id, "write"); err != nil {
		return PutResult{}, err
	}
	result, err := fileInfo(databasePath)
	return PutResult{Info: result}, err
}

func (s *Store) databasePath(id string) string {
	return filepath.Join(s.options.Root, "Databases", id, databaseFilename)
}

func fileInfo(path string) (Info, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	return infoFromStat(stat), nil
}

func infoFromStat(stat os.FileInfo) Info {
	token := fmt.Sprintf("%x-%x", stat.Size(), stat.ModTime().UnixNano())
	return Info{
		Revision: base64.RawURLEncoding.EncodeToString([]byte(token)),
		Length:   stat.Size(),
		Modified: stat.ModTime(),
	}
}

type metadata struct {
	DatabaseID        string `json:"DatabaseId"`
	FirstSeenUnixMS   int64  `json:"FirstSeenUnixMs"`
	LastEvent         string `json:"LastEvent"`
	LastSeenUnixMS    int64  `json:"LastSeenUnixMs"`
	LastWrittenUnixMS int64  `json:"LastWrittenUnixMs,omitempty"`
}

func (s *Store) touch(databasePath, id, event string) error {
	path := filepath.Join(filepath.Dir(databasePath), metadataFilename)
	var value metadata
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &value); err != nil {
			value = metadata{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := s.options.Now().UnixMilli()
	isNew := value.DatabaseID == ""
	previousSeen := value.LastSeenUnixMS
	value.DatabaseID = id
	if value.FirstSeenUnixMS == 0 {
		value.FirstSeenUnixMS = now
	}
	value.LastSeenUnixMS = now
	value.LastEvent = event
	if event == "write" {
		value.LastWrittenUnixMS = now
	}
	if !isNew && event != "write" && now-previousSeen < touchInterval.Milliseconds() {
		return nil
	}
	encoded, err := config.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(path, encoded, 0o600)
}

func (s *Store) createBackup(databasePath string) error {
	directory := filepath.Join(filepath.Dir(databasePath), "ServerBackups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if s.options.BackupInterval > 0 {
		newest, err := newestBackup(directory)
		if err != nil {
			return err
		}
		if newest != "" {
			stat, statErr := os.Stat(newest)
			if statErr != nil {
				return statErr
			}
			if s.options.Now().Sub(stat.ModTime()) < s.options.BackupInterval {
				return nil
			}
		}
	}
	now := s.options.Now()
	name := fmt.Sprintf("clipman-history-%s-%09d.clipdb", now.Format("20060102-150405"), now.UnixNano()%1_000_000_000)
	target := filepath.Join(directory, name)
	if err := copyFile(databasePath, target); err != nil {
		return err
	}
	if err := os.Chtimes(target, now, now); err != nil {
		return err
	}
	return s.pruneBackups(directory, now)
}

func newestBackup(directory string) (string, error) {
	entries, err := filepath.Glob(filepath.Join(directory, "*.clipdb"))
	if err != nil {
		return "", err
	}
	var newest string
	var newestTime time.Time
	for _, path := range entries {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return "", statErr
		}
		if newest == "" || stat.ModTime().After(newestTime) {
			newest, newestTime = path, stat.ModTime()
		}
	}
	return newest, nil
}

func (s *Store) pruneBackups(directory string, now time.Time) error {
	paths, err := filepath.Glob(filepath.Join(directory, "*.clipdb"))
	if err != nil {
		return err
	}
	type item struct {
		path string
		when time.Time
	}
	items := make([]item, 0, len(paths))
	for _, path := range paths {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		if s.options.BackupRetention > 0 && stat.ModTime().Before(now.Add(-s.options.BackupRetention)) {
			if removeErr := os.Remove(path); removeErr != nil {
				return removeErr
			}
			continue
		}
		items = append(items, item{path: path, when: stat.ModTime()})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].when.After(items[right].when) })
	for _, extra := range items[min(s.options.MaxBackups, len(items)):] {
		if err = os.Remove(extra.path); err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return os.Chmod(path, mode)
}

func copyExactly(ctx context.Context, destination io.Writer, source io.Reader, length int64) (int64, error) {
	limited := io.LimitReader(source, length)
	written, err := copyContext(ctx, destination, limited)
	if err != nil {
		return written, err
	}
	if written != length {
		return written, io.ErrUnexpectedEOF
	}
	return written, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func equalFiles(leftPath, rightPath string) (bool, error) {
	leftStat, err := os.Stat(leftPath)
	if err != nil {
		return false, err
	}
	rightStat, err := os.Stat(rightPath)
	if err != nil {
		return false, err
	}
	if leftStat.Size() != rightStat.Size() {
		return false, nil
	}
	left, err := os.Open(leftPath)
	if err != nil {
		return false, err
	}
	defer left.Close()
	right, err := os.Open(rightPath)
	if err != nil {
		return false, err
	}
	defer right.Close()
	leftBuffer := make([]byte, 128*1024)
	rightBuffer := make([]byte, len(leftBuffer))
	for {
		leftRead, leftErr := io.ReadFull(left, leftBuffer)
		rightRead, rightErr := io.ReadFull(right, rightBuffer)
		if leftRead != rightRead || !bytes.Equal(leftBuffer[:leftRead], rightBuffer[:rightRead]) {
			return false, nil
		}
		if errors.Is(leftErr, io.EOF) || errors.Is(leftErr, io.ErrUnexpectedEOF) {
			return errors.Is(rightErr, io.EOF) || errors.Is(rightErr, io.ErrUnexpectedEOF), nil
		}
		if leftErr != nil {
			return false, leftErr
		}
		if rightErr != nil {
			return false, rightErr
		}
	}
}

func copyFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = destination.Close()
		if !committed {
			_ = os.Remove(destinationPath)
		}
	}()
	if _, err = io.CopyBuffer(destination, source, make([]byte, 128*1024)); err == nil {
		err = destination.Sync()
	}
	if closeErr := destination.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	committed = true
	return nil
}

type lockEntry struct {
	mutex sync.Mutex
	refs  int
}

type keyedLocks struct {
	mutex   sync.Mutex
	entries map[string]*lockEntry
}

func (k *keyedLocks) acquire(key string) func() {
	k.mutex.Lock()
	if k.entries == nil {
		k.entries = map[string]*lockEntry{}
	}
	entry := k.entries[key]
	if entry == nil {
		entry = &lockEntry{}
		k.entries[key] = entry
	}
	entry.refs++
	k.mutex.Unlock()
	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		k.mutex.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.entries, key)
		}
		k.mutex.Unlock()
	}
}

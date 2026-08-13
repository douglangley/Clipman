package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/blobstore"
)

const (
	databaseFilename = "clipman-history.clipdb"
	metadataFilename = "clipman-server-metadata.json"
)

type DatabaseInfo struct {
	DatabaseID         string `json:"DatabaseId"`
	Length             int64  `json:"Length"`
	ModifiedUnixMS     int64  `json:"ModifiedUnixMs"`
	FirstSeenUnixMS    int64  `json:"FirstSeenUnixMs"`
	LastSeenUnixMS     int64  `json:"LastSeenUnixMs"`
	LastWrittenUnixMS  int64  `json:"LastWrittenUnixMs"`
	LastActivityUnixMS int64  `json:"LastActivityUnixMs"`
	LastEvent          string `json:"LastEvent"`
	BackupCount        int    `json:"BackupCount"`
	Exists             bool   `json:"Exists"`
}

type BackupInfo struct {
	Name          string `json:"Name"`
	Length        int64  `json:"Length"`
	CreatedUnixMS int64  `json:"CreatedUnixMs"`
	Revision      string `json:"Revision"`
}

type metadata struct {
	FirstSeenUnixMS   int64  `json:"FirstSeenUnixMs"`
	LastSeenUnixMS    int64  `json:"LastSeenUnixMs"`
	LastWrittenUnixMS int64  `json:"LastWrittenUnixMs"`
	LastEvent         string `json:"LastEvent"`
}

type Manager struct {
	Root string
	Now  func() time.Time
}

// ListBackups preserves the legacy authenticated, cross-bucket backup listing.
// Database IDs are deliberately not included in the response.
func (m Manager) ListBackups() ([]BackupInfo, error) {
	paths, err := filepath.Glob(filepath.Join(m.Root, "Databases", "*", "ServerBackups", "*.clipdb"))
	if err != nil {
		return nil, err
	}
	type item struct {
		info BackupInfo
		when time.Time
	}
	items := make([]item, 0, len(paths))
	for _, path := range paths {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return nil, statErr
		}
		token := fmt.Sprintf("%x-%x", stat.Size(), stat.ModTime().UnixNano())
		items = append(items, item{info: BackupInfo{
			Name: filepath.Base(path), Length: stat.Size(), CreatedUnixMS: stat.ModTime().UnixMilli(),
			Revision: base64.RawURLEncoding.EncodeToString([]byte(token)),
		}, when: stat.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].when.After(items[j].when) })
	result := make([]BackupInfo, len(items))
	for index := range items {
		result[index] = items[index].info
	}
	return result, nil
}

func (m Manager) List() ([]DatabaseInfo, error) {
	root := filepath.Join(m.Root, "Databases")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []DatabaseInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !blobstore.ValidDatabaseID(entry.Name()) {
			continue
		}
		bucket := filepath.Join(root, entry.Name())
		db := filepath.Join(bucket, databaseFilename)
		info := DatabaseInfo{DatabaseID: entry.Name()}
		if stat, statErr := os.Stat(db); statErr == nil {
			info.Exists = true
			info.Length = stat.Size()
			info.ModifiedUnixMS = stat.ModTime().UnixMilli()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
		var meta metadata
		if data, readErr := os.ReadFile(filepath.Join(bucket, metadataFilename)); readErr == nil {
			_ = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &meta)
		}
		info.FirstSeenUnixMS = meta.FirstSeenUnixMS
		info.LastSeenUnixMS = meta.LastSeenUnixMS
		info.LastWrittenUnixMS = meta.LastWrittenUnixMS
		info.LastEvent = meta.LastEvent
		info.LastActivityUnixMS = max(info.ModifiedUnixMS, info.LastSeenUnixMS, info.LastWrittenUnixMS)
		if backups, globErr := filepath.Glob(filepath.Join(bucket, "ServerBackups", "*.clipdb")); globErr == nil {
			info.BackupCount = len(backups)
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastActivityUnixMS > result[j].LastActivityUnixMS })
	return result, nil
}

func (m Manager) Delete(id string, forceRecent bool) (string, error) {
	if !blobstore.ValidDatabaseID(id) {
		return "", fmt.Errorf("invalid database ID: %s", id)
	}
	items, err := m.List()
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.DatabaseID == id && !forceRecent && m.now().Sub(time.UnixMilli(item.LastActivityUnixMS)) < 24*time.Hour {
			return "", fmt.Errorf("refusing to move recently active database bucket %s", id)
		}
	}
	source := filepath.Join(m.Root, "Databases", id)
	stat, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("database bucket not found: %s", id)
	}
	if err != nil {
		return "", err
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("database bucket not found: %s", id)
	}
	targetRoot := filepath.Join(m.Root, "DeletedDatabases")
	if err = os.MkdirAll(targetRoot, 0o700); err != nil {
		return "", err
	}
	base := id + "-" + m.now().Format("20060102-150405")
	target := filepath.Join(targetRoot, base)
	for n := 2; ; n++ {
		if _, statErr := os.Stat(target); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		target = filepath.Join(targetRoot, fmt.Sprintf("%s-%d", base, n))
	}
	if err = os.Rename(source, target); err != nil {
		return "", err
	}
	return target, nil
}

func (m Manager) Stale(days int) ([]DatabaseInfo, error) {
	if days <= 0 {
		return nil, errors.New("days must be greater than zero")
	}
	items, err := m.List()
	if err != nil {
		return nil, err
	}
	cutoff := m.now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	out := []DatabaseInfo{}
	for _, item := range items {
		if item.LastActivityUnixMS > 0 && item.LastActivityUnixMS < cutoff {
			out = append(out, item)
		}
	}
	return out, nil
}
func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

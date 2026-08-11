package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPutHeadGetAndConditionalUpdates(t *testing.T) {
	now := time.Unix(1_700_000_000, 123)
	store, err := New(Options{Root: t.TempDir(), MaxDatabaseBytes: 1024, CreateBackupBeforeWrite: true, BackupInterval: 0, BackupRetention: time.Hour, MaxBackups: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 43)
	first, err := store.Put(context.Background(), id, bytes.NewReader([]byte("one")), 3, Conditions{CreateOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Info.Length != 3 || first.Info.Revision == "" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	var downloaded bytes.Buffer
	got, n, err := store.Get(context.Background(), id, nil, &downloaded)
	if err != nil || n != 3 || downloaded.String() != "one" || got.Revision != first.Info.Revision {
		t.Fatalf("get: info=%+v n=%d data=%q err=%v", got, n, downloaded.String(), err)
	}
	_, err = store.Put(context.Background(), id, bytes.NewReader([]byte("two")), 3, Conditions{CreateOnly: true})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Status != 412 {
		t.Fatalf("expected 412, got %v", err)
	}
	_, err = store.Put(context.Background(), id, bytes.NewReader([]byte("two")), 3, Conditions{Match: "wrong"})
	if !errors.As(err, &conflict) || conflict.Status != 409 {
		t.Fatalf("expected 409, got %v", err)
	}
	now = now.Add(time.Second)
	second, err := store.Put(context.Background(), id, bytes.NewReader([]byte("two")), 3, Conditions{Match: first.Info.Revision})
	if err != nil || second.Info.Revision == first.Info.Revision {
		t.Fatalf("update=%+v err=%v", second, err)
	}
}

func TestShortUploadDoesNotCreateDatabase(t *testing.T) {
	store, _ := New(Options{Root: t.TempDir(), MaxDatabaseBytes: 1024})
	id := strings.Repeat("b", 43)
	_, err := store.Put(context.Background(), id, bytes.NewReader([]byte("x")), 2, Conditions{CreateOnly: true})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
	if _, err = store.Head(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("database created after failed upload: %v", err)
	}
}

func TestDatabaseIDValidation(t *testing.T) {
	if !ValidDatabaseID(strings.Repeat("z", 43)) || ValidDatabaseID("../bad") || ValidDatabaseID(strings.Repeat("z", 31)) {
		t.Fatal("database ID validation mismatch")
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func TestLargeTransferUsesStreamingPath(t *testing.T) {
	if testing.Short() {
		t.Skip("64 MiB release-gate transfer")
	}
	const size int64 = 64 << 20
	store, err := New(Options{Root: t.TempDir(), MaxDatabaseBytes: size})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("l", 43)
	result, err := store.Put(context.Background(), id, io.LimitReader(zeroReader{}, size), size, Conditions{CreateOnly: true})
	if err != nil || result.Info.Length != size {
		t.Fatalf("put info=%+v err=%v", result.Info, err)
	}
	info, written, err := store.Get(context.Background(), id, nil, io.Discard)
	if err != nil || written != size || info.Length != size {
		t.Fatalf("get info=%+v written=%d err=%v", info, written, err)
	}
	store.locks.mutex.Lock()
	remaining := len(store.locks.entries)
	store.locks.mutex.Unlock()
	if remaining != 0 {
		t.Fatalf("keyed locks retained %d entries", remaining)
	}
}

func TestSeparateBucketsUpdateIndependently(t *testing.T) {
	store, _ := New(Options{Root: t.TempDir(), MaxDatabaseBytes: 1024})
	ids := []string{strings.Repeat("x", 43), strings.Repeat("y", 43)}
	var wg sync.WaitGroup
	errorsCh := make(chan error, len(ids))
	for index, id := range ids {
		wg.Add(1)
		go func(id string, value byte) {
			defer wg.Done()
			_, err := store.Put(context.Background(), id, bytes.NewReader([]byte{value}), 1, Conditions{CreateOnly: true})
			errorsCh <- err
		}(id, byte(index))
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids {
		if _, err := store.Head(id); err != nil {
			t.Fatalf("bucket %s: %v", id, err)
		}
	}
}

func TestCancelledUploadCleansStagingFile(t *testing.T) {
	root := t.TempDir()
	store, _ := New(Options{Root: root, MaxDatabaseBytes: 1024})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Put(ctx, strings.Repeat("q", 43), zeroReader{}, 1024, Conditions{CreateOnly: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	uploads := filepath.Join(root, ".uploads")
	entries, readErr := os.ReadDir(uploads)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled upload left %d staging files", len(entries))
	}
}

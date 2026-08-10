package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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

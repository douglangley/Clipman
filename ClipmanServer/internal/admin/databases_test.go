package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListDeleteAndStale(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	id := strings.Repeat("a", 43)
	bucket := filepath.Join(root, "Databases", id)
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(bucket, databaseFilename)
	if err := os.WriteFile(db, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(db, old, old); err != nil {
		t.Fatal(err)
	}
	m := Manager{Root: root, Now: func() time.Time { return now }}
	items, err := m.List()
	if err != nil || len(items) != 1 || items[0].Length != 4 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	stale, err := m.Stale(1)
	if err != nil || len(stale) != 1 {
		t.Fatalf("stale=%+v err=%v", stale, err)
	}
	target, err := m.Delete(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); err != nil {
		t.Fatal(err)
	}
}

func TestRecentDeleteRequiresForce(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("b", 43)
	bucket := filepath.Join(root, "Databases", id)
	_ = os.MkdirAll(bucket, 0o700)
	_ = os.WriteFile(filepath.Join(bucket, databaseFilename), []byte("x"), 0o600)
	m := Manager{Root: root}
	if _, err := m.Delete(id, false); err == nil {
		t.Fatal("recent bucket was deleted")
	}
	if _, err := m.Delete(id, true); err != nil {
		t.Fatal(err)
	}
}

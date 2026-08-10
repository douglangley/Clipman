package dataroot

import (
	"os"
	"runtime"
	"testing"
)

func TestExclusiveLockAndRelease(t *testing.T) {
	root := t.TempDir()
	if runtime.GOOS == "windows" {
		var err error
		root, err = os.MkdirTemp(".", ".test-tmp-dataroot-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	first, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path() == "" {
		t.Fatal("lock path is empty")
	}
	if second, secondErr := Acquire(root); secondErr == nil {
		_ = second.Close()
		t.Fatal("second data-root lock unexpectedly succeeded")
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := Acquire(root)
	if err != nil {
		t.Fatalf("lock did not release: %v", err)
	}
	if err = third.Close(); err != nil {
		t.Fatal(err)
	}
}

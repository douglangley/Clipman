package dataroot

import (
	"os"
	"path/filepath"
)

type Lock struct {
	file *os.File
	path string
}

func Acquire(root string) (*Lock, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(resolved, ".clipman-server.lock")
	marker, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := marker.Stat()
	if err == nil && info.Size() == 0 {
		_, err = marker.WriteAt([]byte("0\n"), 0)
		if err == nil {
			err = marker.Sync()
		}
	}
	closeErr := marker.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	file, err := openLockedFile(path)
	if err != nil {
		return nil, err
	}
	return &Lock{file: file, path: path}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unlockFile(file)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

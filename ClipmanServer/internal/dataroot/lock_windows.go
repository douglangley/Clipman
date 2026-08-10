//go:build windows

package dataroot

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(file *os.File) error {
	return nil
}

func openLockedFile(path string) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func unlockFile(file *os.File) error {
	return nil
}

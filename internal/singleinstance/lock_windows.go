//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile opens path with dwShareMode 0, so any second CreateFile on it
// (from this or another process) fails with ERROR_SHARING_VIOLATION until
// the handle closes - which Windows does for us when the process dies.
func lockFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("singleinstance: lock path: %w", err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("singleinstance: open lock file: %w", err)
	}
	return os.NewFile(uintptr(h), path), nil
}

func unlockFile(f *os.File) error {
	return f.Close()
}

// errSharingViolation is ERROR_SHARING_VIOLATION (32), which package
// syscall does not export.
const errSharingViolation syscall.Errno = 32

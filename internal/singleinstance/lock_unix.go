//go:build !windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("singleinstance: open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("singleinstance: flock: %w", err)
	}
	return f, nil
}

func unlockFile(f *os.File) error {
	// Closing the descriptor releases the flock.
	return f.Close()
}

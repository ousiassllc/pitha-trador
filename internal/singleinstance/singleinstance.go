// Package singleinstance is a cross-process, advisory "only one of me"
// lock backed by a file the OS releases automatically when the holding
// process exits or crashes (no stale lock cleanup needed).
//
// cmd/desktop takes it before bootstrap.Run so a second launch (e.g. the
// desktop icon clicked while the --supervise autostart instance is
// already running) exits before it can recover the first instance's
// running jobs, start a second Scheduler/Kill Switch or place duplicate
// orders against the shared SQLite DB and kabuステーション API
// (docs/requirements/non-functional.md §3).
package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrAlreadyRunning is returned by Acquire when another live process
// already holds the lock.
var ErrAlreadyRunning = errors.New("singleinstance: another instance is already running")

// Lock is a held single-instance lock. The zero value is not usable.
type Lock struct {
	file *os.File
}

// Acquire takes the exclusive lock stored at path (creating the file and
// its parent directory if needed). It never blocks: it returns
// ErrAlreadyRunning when another process holds the lock.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("singleinstance: create lock dir: %w", err)
	}
	f, err := lockFile(path)
	if err != nil {
		return nil, err
	}
	return &Lock{file: f}, nil
}

// Release drops the lock. Safe to call more than once and on a nil Lock.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	l.file = nil
	return err
}

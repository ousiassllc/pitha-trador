package main

import (
	"os"
	"path/filepath"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/singleinstance"
)

// Lock file names, stored next to the SQLite DB (see lockPath).
// The app and the --supervise watcher take separate locks: the autostart
// watcher and the app it spawns are different processes that both run at
// once, while two watchers (or two apps) must never coexist.
const (
	appLockName        = "app.lock"
	supervisorLockName = "supervisor.lock"
)

// lockPath places the named lock next to the SQLite DB (EnvDBPath or
// bootstrap.DefaultDBPath, the same resolution bootstrap.Run applies):
// the DB is the shared resource two instances must not both drive.
func lockPath(name string) (string, error) {
	dbPath := os.Getenv(bootstrap.EnvDBPath)
	if dbPath == "" {
		var err error
		if dbPath, err = bootstrap.DefaultDBPath(); err != nil {
			return "", err
		}
	}
	return filepath.Join(filepath.Dir(dbPath), name), nil
}

// acquireInstanceLock takes the named single-instance lock. It returns
// singleinstance.ErrAlreadyRunning when another process already holds it.
func acquireInstanceLock(name string) (*singleinstance.Lock, error) {
	path, err := lockPath(name)
	if err != nil {
		return nil, err
	}
	return singleinstance.Acquire(path)
}

package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/singleinstance"
)

func TestAcquireInstanceLock_SecondLaunchIsRejected(t *testing.T) {
	t.Setenv(bootstrap.EnvDBPath, filepath.Join(t.TempDir(), "pitha.db"))

	first, err := acquireInstanceLock(appLockName)
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	defer func() { _ = first.Release() }()

	if _, err := acquireInstanceLock(appLockName); !errors.Is(err, singleinstance.ErrAlreadyRunning) {
		t.Fatalf("second launch err = %v, want ErrAlreadyRunning", err)
	}

	// The supervisor watcher must coexist with the app it spawns.
	sup, err := acquireInstanceLock(supervisorLockName)
	if err != nil {
		t.Fatalf("supervisor lock alongside app lock: %v", err)
	}
	_ = sup.Release()
}

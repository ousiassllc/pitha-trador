package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/singleinstance"
)

// A second server on the same DB must be rejected by the app.lock guard
// before bootstrap.Run opens/migrates the DB (and so before
// Services.Start could recover the first instance's running jobs).
func TestRun_SecondInstanceExitsBeforeBootstrap(t *testing.T) {
	t.Setenv(startup.EnvLogDir, filepath.Join(t.TempDir(), "logs"))
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	t.Setenv(bootstrap.EnvDBPath, dbPath)

	first, err := bootstrap.AcquireInstanceLock(bootstrap.AppLockName)
	if err != nil {
		t.Fatalf("first instance lock: %v", err)
	}
	defer func() { _ = first.Release() }()

	if err := run(); !errors.Is(err, singleinstance.ErrAlreadyRunning) {
		t.Fatalf("run() err = %v, want ErrAlreadyRunning", err)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("DB file stat err = %v, want not-exist (bootstrap.Run must not be reached)", err)
	}
}

package singleinstance

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquire_SecondAcquireFailsUntilReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	if _, err := Acquire(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Acquire err = %v, want ErrAlreadyRunning", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("second Release should be a no-op, got %v", err)
	}

	again, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = again.Release()
}

func TestAcquire_DifferentPathsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	a, err := Acquire(filepath.Join(dir, "app.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Release() }()
	b, err := Acquire(filepath.Join(dir, "supervisor.lock"))
	if err != nil {
		t.Fatalf("independent lock should succeed: %v", err)
	}
	_ = b.Release()
}

func TestRelease_NilLock(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

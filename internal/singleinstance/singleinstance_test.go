package singleinstance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestAcquire_CreatesOwnerOnlyDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "app.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Release() })

	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %q: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%q mode = %o, want %o", p, got, want)
		}
	}
}

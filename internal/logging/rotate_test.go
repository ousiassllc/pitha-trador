package logging

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRotatingWriter_WritesIntoTodaysFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotatingWriter(dir)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	fixed := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return fixed }

	if _, err := w.Write([]byte("first line\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := filepath.Join(dir, "2026-09-27.log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected log file %q: %v", want, err)
	}
	if string(data) != "first line\n" {
		t.Errorf("log file content = %q, want %q", data, "first line\n")
	}
}

func TestRotatingWriter_RollsOverAtDateChange(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotatingWriter(dir)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	day1 := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	w.now = func() time.Time { return day1 }
	if _, err := w.Write([]byte("day1\n")); err != nil {
		t.Fatalf("Write day1: %v", err)
	}

	day2 := day1.Add(2 * time.Minute) // 2026-09-28 00:01 UTC
	w.now = func() time.Time { return day2 }
	if _, err := w.Write([]byte("day2\n")); err != nil {
		t.Fatalf("Write day2: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	day1Data, err := os.ReadFile(filepath.Join(dir, "2026-09-27.log"))
	if err != nil {
		t.Fatalf("expected day1 log file: %v", err)
	}
	if string(day1Data) != "day1\n" {
		t.Errorf("day1 log content = %q, want %q", day1Data, "day1\n")
	}

	day2Data, err := os.ReadFile(filepath.Join(dir, "2026-09-28.log"))
	if err != nil {
		t.Fatalf("expected day2 log file: %v", err)
	}
	if string(day2Data) != "day2\n" {
		t.Errorf("day2 log content = %q, want %q", day2Data, "day2\n")
	}
}

func TestRotatingWriter_AppendsAcrossWritesSameDay(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotatingWriter(dir)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	fixed := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return fixed }

	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-09-27.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if string(data) != "a\nb\n" {
		t.Errorf("log file content = %q, want %q", data, "a\nb\n")
	}
}

func TestRotatingWriter_CreatesOwnerOnlyDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	// A directory left at 0755 by an older version must be tightened too.
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	w, err := NewRotatingWriter(dir)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.now = func() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	assertMode(t, dir, 0o700)
	assertMode(t, filepath.Join(dir, "2026-09-27.log"), 0o600)
}

func TestArchiver_CreatesOwnerOnlyArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-08-01.log", "old content\n")

	a := NewArchiver(dir, 30)
	a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	if err := a.Archive(context.Background()); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	assertMode(t, filepath.Join(dir, "2026-08-01.log.gz"), 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%q mode = %o, want %o", path, got, want)
	}
}

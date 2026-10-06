package logging

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLogFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

func TestArchiver_CompressesFilesOlderThanRetention(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-08-01.log", "old content\n")    // 57 days before "now" below
	writeLogFile(t, dir, "2026-09-20.log", "recent content\n") // 7 days before "now"

	a := NewArchiver(dir, 30)
	a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }

	if err := a.Archive(context.Background()); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "2026-08-01.log")); !os.IsNotExist(err) {
		t.Errorf("expected 2026-08-01.log to be removed after archival, stat err = %v", err)
	}
	gzPath := filepath.Join(dir, "2026-08-01.log.gz")
	f, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("expected archive %q: %v", gzPath, err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read decompressed archive: %v", err)
	}
	if string(data) != "old content\n" {
		t.Errorf("decompressed archive content = %q, want %q", data, "old content\n")
	}

	if _, err := os.Stat(filepath.Join(dir, "2026-09-20.log")); err != nil {
		t.Errorf("expected recent 2026-09-20.log to remain untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-09-20.log.gz")); !os.IsNotExist(err) {
		t.Errorf("recent log file should not have been archived")
	}
}

func TestArchiver_IgnoresNonLogFiles(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "README.md", "not a log file")
	writeLogFile(t, dir, "2026-01-01.log.gz", "already archived")

	a := NewArchiver(dir, 30)
	a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }

	if err := a.Archive(context.Background()); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("README.md should have been left untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-01-01.log.gz")); err != nil {
		t.Errorf("already-archived file should have been left untouched: %v", err)
	}
}

func TestArchiver_CompressesOldErrorLogsToo(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-08-01-error.log", "old error\n")
	writeLogFile(t, dir, "2026-09-20-error.log", "recent error\n")

	a := NewArchiver(dir, 30)
	a.now = func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	if err := a.Archive(context.Background()); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "2026-08-01-error.log.gz")); err != nil {
		t.Errorf("old error log was not archived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-08-01-error.log")); !os.IsNotExist(err) {
		t.Errorf("old error log was not removed after archival: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-09-20-error.log")); err != nil {
		t.Errorf("recent error log should remain: %v", err)
	}
}

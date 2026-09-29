package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// countRows opens a backup copy and counts the marker rows written before
// the backup.
func countRows(t *testing.T, dbPath string) int {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var n int
	if err := conn.QueryRow("SELECT count(*) FROM backup_marker").Scan(&n); err != nil {
		t.Fatalf("query backup: %v", err)
	}
	return n
}

func TestBackup_CopiesUnflushedWALContentsIntoDailyFile(t *testing.T) {
	conn := openTestDB(t)
	if _, err := conn.Exec("CREATE TABLE backup_marker (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("INSERT INTO backup_marker VALUES (1), (2), (3)"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	s := New(conn, dir, 0)
	s.now = func() time.Time { return time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC) } // Tuesday

	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	daily := filepath.Join(dir, "daily", "pitha-2026-09-29.db")
	if got := countRows(t, daily); got != 3 {
		t.Fatalf("backup rows = %d, want 3", got)
	}
	if exists(daily + ".tmp") {
		t.Error("temporary file left behind")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "weekly")); len(entries) != 0 {
		t.Errorf("weekly archive written on a Tuesday: %v", entries)
	}

	// Re-running on the same day replaces the copy instead of failing.
	if _, err := conn.Exec("INSERT INTO backup_marker VALUES (4)"); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("second Backup: %v", err)
	}
	if got := countRows(t, daily); got != 4 {
		t.Fatalf("backup rows after re-run = %d, want 4", got)
	}
}

func TestBackup_PrunesDailyOlderThanRetentionButKeepsWeeklyArchives(t *testing.T) {
	conn := openTestDB(t)
	dir := t.TempDir()
	s := New(conn, dir, 90)
	s.now = func() time.Time { return time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC) }

	daily := filepath.Join(dir, "daily")
	touch(t, filepath.Join(daily, "pitha-2026-06-30.db")) // 91 days old -> pruned
	touch(t, filepath.Join(daily, "pitha-2026-07-01.db")) // exactly 90 days old -> kept
	touch(t, filepath.Join(daily, "notes.txt"))           // foreign file -> untouched
	touch(t, filepath.Join(dir, "weekly", "pitha-2025-01-05.db.gz"))

	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if exists(filepath.Join(daily, "pitha-2026-06-30.db")) {
		t.Error("91-day-old daily backup should be pruned")
	}
	for _, keep := range []string{
		filepath.Join(daily, "pitha-2026-07-01.db"),
		filepath.Join(daily, "notes.txt"),
		filepath.Join(daily, "pitha-2026-09-29.db"),
		filepath.Join(dir, "weekly", "pitha-2025-01-05.db.gz"),
	} {
		if !exists(keep) {
			t.Errorf("%s should be kept", keep)
		}
	}
}

func TestBackup_WritesWeeklyGzipArchiveOnSunday(t *testing.T) {
	conn := openTestDB(t)
	if _, err := conn.Exec("CREATE TABLE backup_marker (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := New(conn, dir, 0)
	s.now = func() time.Time { return time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC) } // Sunday

	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	archive := filepath.Join(dir, "weekly", "pitha-2026-09-27.db.gz")
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("weekly archive missing: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	restored := filepath.Join(t.TempDir(), "restored.db")
	out, err := os.Create(restored)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := gz.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				t.Fatal(err)
			}
		}
		if rerr != nil {
			break
		}
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, restored); got != 0 {
		t.Fatalf("restored rows = %d, want 0 (table present, empty)", got)
	}
}

func TestBackup_ReturnsErrorWhenDestinationIsNotADirectory(t *testing.T) {
	conn := openTestDB(t)
	file := filepath.Join(t.TempDir(), "afile")
	touch(t, file)

	if err := New(conn, file, 0).Backup(context.Background()); err == nil {
		t.Fatal("Backup should fail when the destination path is a file")
	}
}

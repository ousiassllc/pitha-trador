package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
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
	// The week's archive is created by its first backup, whatever the weekday.
	if entries, _ := os.ReadDir(filepath.Join(dir, "weekly")); len(entries) != 1 || entries[0].Name() != "pitha-2026-09-28.db.gz" {
		t.Errorf("weekly entries on a Tuesday = %v, want only pitha-2026-09-28.db.gz (that ISO week's Monday)", entries)
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

func TestBackup_PrunesDailyOlderThanRetentionAndWeeklyOlderThan52Weeks(t *testing.T) {
	conn := openTestDB(t)
	dir := t.TempDir()
	s := New(conn, dir, 90)
	s.now = func() time.Time { return time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC) }

	daily := filepath.Join(dir, "daily")
	touch(t, filepath.Join(daily, "pitha-2026-06-30.db"))            // 91 days old -> pruned
	touch(t, filepath.Join(daily, "pitha-2026-07-01.db"))            // exactly 90 days old -> kept
	touch(t, filepath.Join(daily, "notes.txt"))                      // foreign file -> untouched
	touch(t, filepath.Join(dir, "weekly", "pitha-2025-09-27.db.gz")) // 367 days old -> pruned
	touch(t, filepath.Join(dir, "weekly", "pitha-2025-09-30.db.gz")) // 364 days old (52 weeks) -> kept

	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if exists(filepath.Join(daily, "pitha-2026-06-30.db")) {
		t.Error("91-day-old daily backup should be pruned")
	}
	if exists(filepath.Join(dir, "weekly", "pitha-2025-09-27.db.gz")) {
		t.Error("weekly archive older than 52 weeks should be pruned")
	}
	for _, keep := range []string{
		filepath.Join(daily, "pitha-2026-07-01.db"),
		filepath.Join(daily, "notes.txt"),
		filepath.Join(daily, "pitha-2026-09-29.db"),
		filepath.Join(dir, "weekly", "pitha-2025-09-30.db.gz"),
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

	archive := filepath.Join(dir, "weekly", "pitha-2026-09-21.db.gz") // Monday of the ISO week ending 09-27
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

func TestBackup_ScrubsSecretsAndRestrictsPermissions(t *testing.T) {
	conn := openTestDB(t)
	if _, err := conn.Exec(`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES ('KABU_API_PASSWORD', 'topsecretcipher', '2026-09-29T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('keep.me', '"v"', '2026-09-29T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	// A pre-existing world-readable destination layout must be tightened.
	if err := os.MkdirAll(filepath.Join(dir, "daily"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(conn, dir, 0)
	s.now = func() time.Time { return time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC) } // Sunday

	if err := s.Backup(context.Background()); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	daily := filepath.Join(dir, "daily", "pitha-2026-09-27.db")
	backup, err := sql.Open("sqlite", "file:"+daily)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backup.Close() }()
	var secrets, settings int
	if err := backup.QueryRow("SELECT count(*) FROM secrets").Scan(&secrets); err != nil {
		t.Fatal(err)
	}
	if err := backup.QueryRow("SELECT count(*) FROM runtime_settings WHERE key = 'keep.me'").Scan(&settings); err != nil {
		t.Fatal(err)
	}
	if secrets != 0 {
		t.Errorf("backup secrets rows = %d, want 0", secrets)
	}
	if settings != 1 {
		t.Errorf("non-secret data must survive the scrub: runtime_settings rows = %d, want 1", settings)
	}
	raw, err := os.ReadFile(daily)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("topsecretcipher")) {
		t.Error("secret ciphertext still present in the backup file (free pages not rewritten)")
	}
	// The live database keeps its secrets.
	var live int
	if err := conn.QueryRow("SELECT count(*) FROM secrets").Scan(&live); err != nil || live != 1 {
		t.Errorf("live secrets rows = %d (err %v), want 1", live, err)
	}

	for path, want := range map[string]os.FileMode{
		filepath.Join(dir, "daily"):  0o700,
		filepath.Join(dir, "weekly"): 0o700,
		daily:                        0o600,
		filepath.Join(dir, "weekly", "pitha-2026-09-21.db.gz"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "daily")); len(entries) != 1 {
		t.Errorf("sidecar files left in daily/: %v", entries)
	}
}

func TestBackup_FailsWithoutCreatingMissingDestination(t *testing.T) {
	conn := openTestDB(t)
	dir := filepath.Join(t.TempDir(), "unmounted")

	if err := New(conn, dir, 0).Backup(context.Background()); err == nil {
		t.Fatal("Backup should fail when the destination directory does not exist")
	}
	if exists(dir) {
		t.Error("Backup must not create the missing destination directory")
	}
}

func TestBackup_LeavesNoTemporaryFileWhenFinalizeFails(t *testing.T) {
	conn := openTestDB(t)
	dir := t.TempDir()
	// A directory squatting on the final name makes finalizing fail after
	// the copy; nothing may be left behind under the temporary name.
	final := filepath.Join(dir, "daily", "pitha-2026-09-29.db")
	if err := os.MkdirAll(filepath.Join(final, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(conn, dir, 0)
	s.now = func() time.Time { return time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC) }

	if err := s.Backup(context.Background()); err == nil {
		t.Fatal("Backup should fail when the copy cannot be finalized")
	}
	if exists(final + ".tmp") {
		t.Error("temporary file left behind after a failed backup")
	}
}

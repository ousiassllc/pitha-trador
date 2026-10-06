package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// Issue #591: '#', '?' and '%XX' in the operator-chosen backup directory
// must not make scrub/verify open a different file than the .tmp copy.
func TestBackup_DirectoryWithURIMetacharacters(t *testing.T) {
	for _, name := range []string{"dir#1", "dir?x", "dir%41", "a b#c?d%e"} {
		t.Run(name, func(t *testing.T) {
			conn := openTestDB(t)
			if _, err := conn.Exec(`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES ('K', 'cipher', '2026-09-29T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			dir := filepath.Join(parent, name)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			s := New(conn, dir, 0)
			s.now = func() time.Time { return time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC) }

			if err := s.Backup(context.Background()); err != nil {
				t.Fatalf("Backup(%q): %v", dir, err)
			}

			daily := filepath.Join(dir, "daily", "pitha-2026-10-06.db")
			copyDB, err := sql.Open("sqlite", sqlitedb.FileURI(daily, "mode=ro"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = copyDB.Close() }()
			var secrets int
			if err := copyDB.QueryRow("SELECT count(*) FROM secrets").Scan(&secrets); err != nil {
				t.Fatalf("query backup: %v", err)
			}
			if secrets != 0 {
				t.Errorf("backup secrets rows = %d, want 0", secrets)
			}
			var integrity string
			if err := copyDB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
				t.Errorf("integrity_check = %q (err %v), want ok", integrity, err)
			}

			// Nothing may be created beside the backup directory.
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != name {
				t.Errorf("parent directory entries = %v, want only %q", entries, name)
			}
		})
	}
}

// A path that does not resolve to an existing copy must fail instead of
// creating an empty SQLite file there (mode=rw).
func TestOpenBackupCopy_DoesNotCreateMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	err := verifyCopy(context.Background(), missing)
	if err == nil {
		t.Fatal("verifyCopy of a missing file succeeded")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Errorf("verifyCopy created %q (stat err %v)", missing, statErr)
	}
}

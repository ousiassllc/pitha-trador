package sqlitedb_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func TestOpenReadOnly_ReadsButNeverWritesOrCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ro, err := sqlitedb.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	var n int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM runtime_settings`).Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := ro.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('k', '1', 'now')`); err == nil {
		t.Error("a write through a read-only connection should fail")
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := sqlitedb.OpenReadOnly(missing); err == nil {
		t.Error("OpenReadOnly of a missing file should fail")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("OpenReadOnly must not create the file")
	}
}

// dirEntries lists the names in dir, sorted.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// A reader of a WAL database needs the -shm index (and an empty -wal) next to
// the file, and SQLite leaves them behind when the read-only connection
// closes (it cannot checkpoint or delete them). The test pins exactly that:
// the database file is byte-identical, the only new entries are the -wal /
// -shm side files, and the -wal holds no frames. The docs say "the database
// file is not modified" for this reason, not "nothing is written".
func TestOpenReadOnly_OnlySideFilesAppear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	before := dirEntries(t, dir)
	dbBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	ro, err := sqlitedb.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	var n int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM runtime_settings`).Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := ro.Close(); err != nil {
		t.Fatalf("Close read-only: %v", err)
	}

	for _, name := range dirEntries(t, dir) {
		if !slices.Contains(before, name) && name != "wal.db-wal" && name != "wal.db-shm" {
			t.Errorf("unexpected new file %q (before %v)", name, before)
		}
	}
	dbAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dbBefore, dbAfter) {
		t.Error("the database file changed")
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() != 0 {
		t.Errorf("-wal holds %d bytes, want an empty file", info.Size())
	}
}

// A path with URI metacharacters ('?', '#', '%') must reach SQLite verbatim.
func TestOpenReadOnly_PathWithURIMetacharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%41")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "d?e#f%42.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ro, err := sqlitedb.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	var n int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM runtime_settings`).Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := dirEntries(t, dir); len(got) == 0 || got[0] != "d?e#f%42.db" {
		t.Errorf("unexpected directory listing %v (the database must not be duplicated under an unescaped name)", got)
	}
}

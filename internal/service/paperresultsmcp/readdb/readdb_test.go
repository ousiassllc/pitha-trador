package readdb_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/readdb"
)

// fixture builds a migrated database file (test-only use of sqlitedb.Open).
func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pitha.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func exec(db *sql.DB, q string) error {
	_, err := db.ExecContext(context.Background(), q)
	return err
}

// The MCP's connection is read-only: INSERT, UPDATE, DELETE, CREATE, DROP
// and ALTER all fail, and the data is unchanged afterwards.
func TestOpenIsReadOnlyAndQueryOnly(t *testing.T) {
	db, err := readdb.Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	writes := map[string]string{
		"insert": `INSERT INTO instruments (symbol, name, market) VALUES ('9999', 'x', 'TSE')`,
		"update": `UPDATE instruments SET name = 'hacked'`,
		"delete": `DELETE FROM instruments`,
		"create": `CREATE TABLE evil (id INTEGER)`,
		"drop":   `DROP TABLE instruments`,
		"alter":  `ALTER TABLE instruments ADD COLUMN evil TEXT`,
		"secret": `INSERT INTO secrets (key, encrypted_value, updated_at) VALUES ('k', 'v', 't')`,
	}
	for name, q := range writes {
		if err := exec(db, q); err == nil {
			t.Errorf("%s succeeded on the MCP connection", name)
		}
	}
	// A PRAGMA query_only=OFF would lift the second guard; mode=ro still
	// holds, so the write must still fail even then.
	_ = exec(db, `PRAGMA query_only=OFF`)
	if err := exec(db, `INSERT INTO instruments (symbol, name, market) VALUES ('9999', 'x', 'TSE')`); err == nil {
		t.Error("insert succeeded after query_only=OFF: mode=ro is not enforced")
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM instruments WHERE id = 1`).Scan(&name); err != nil || name != "n" {
		t.Fatalf("row changed or unreadable: %q, %v", name, err)
	}
}

// query_only is on for every pooled connection, busy_timeout is 1 s and the
// pool holds one connection.
func TestOpenPragmasAndPool(t *testing.T) {
	db, err := readdb.Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	var qo, busy int
	if err := db.QueryRow(`PRAGMA query_only`).Scan(&qo); err != nil || qo != 1 {
		t.Fatalf("query_only = %d, %v", qo, err)
	}
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil || busy != 1000 {
		t.Fatalf("busy_timeout = %d, %v; want 1000", busy, err)
	}
	if readdb.BusyTimeout.Milliseconds() != 1000 {
		t.Fatalf("BusyTimeout = %v", readdb.BusyTimeout)
	}
}

// A missing file is db_unavailable material and is never created, neither
// the file nor its WAL/SHM companions.
func TestOpenMissingFileIsUnavailableAndNotCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pitha.db")
	db, err := readdb.Open(path)
	if db != nil {
		_ = db.Close()
		t.Fatal("Open returned a handle for a missing file")
	}
	if !errors.Is(err, readdb.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Open created files: %v", entries)
	}

	if _, err := readdb.Open(dir); !errors.Is(err, readdb.ErrUnavailable) {
		t.Fatalf("directory: err = %v, want ErrUnavailable", err)
	}
}

// Not a database at all: unavailable, not a panic or a created file.
func TestOpenGarbageFileIsUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pitha.db")
	if err := os.WriteFile(path, []byte("this is not sqlite, just text padding........................"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := readdb.Open(path)
	if err == nil {
		// Opening is lazy for a bad header; the first read must fail.
		defer func() { _ = db.Close() }()
		if _, qerr := db.Exec(`SELECT 1 FROM instruments`); qerr == nil {
			t.Fatal("reading a non-database file succeeded")
		}
		return
	}
	if !errors.Is(err, readdb.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

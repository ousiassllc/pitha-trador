package sidecar_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb/sidecar"
)

// A sidecar write is visible to the application's connection, never creates the
// file, and neither its failures nor its successes touch DBWriteFailures (the
// DB-write kill switch's streak).
func TestOpen_WritesTheSameFileWithoutFeedingDBWriteFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "side.db")
	app, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	side, err := sidecar.Open(path)
	if err != nil {
		t.Fatalf("sidecar.Open: %v", err)
	}
	t.Cleanup(func() { _ = side.Close() })

	sqlitedb.DBWriteFailures.Fail()
	before := sqlitedb.DBWriteFailures.ConsecutiveFailures()
	if _, err := side.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('side', '1', 'now')`); err != nil {
		t.Fatalf("sidecar write: %v", err)
	}
	if _, err := side.Exec(`INSERT INTO no_such_table (x) VALUES (1)`); err == nil {
		t.Fatal("a write to a missing table succeeded")
	}
	if got := sqlitedb.DBWriteFailures.ConsecutiveFailures(); got != before {
		t.Fatalf("DBWriteFailures streak = %d after sidecar statements, want %d untouched", got, before)
	}
	sqlitedb.DBWriteFailures.Succeed()

	var value string
	if err := app.QueryRow(`SELECT value FROM runtime_settings WHERE key = 'side'`).Scan(&value); err != nil || value != "1" {
		t.Fatalf("the application connection reads %q, %v; want the sidecar's row", value, err)
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := sidecar.Open(missing); err == nil {
		t.Error("sidecar.Open of a missing file should fail")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("sidecar.Open must not create the file")
	}
}

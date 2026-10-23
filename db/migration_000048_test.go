package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000048 (issue #885) creates jev_model_changes: one row per switch
// of the configured Jev model. The actor is 'gui', the values may be NULL
// (unset) but a row that changes nothing is refused, and one down step drops
// the table and nothing else. It migrates 43 -> 48 and steps one back, so it
// does not depend on whether 44-47 (other changes) are present.
func TestMigration000048_RoundTripsAndConstrainsTheJevModelRecord(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	exists := func(table string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}
	if exists("jev_model_changes") {
		t.Fatal("jev_model_changes exists before migration 48")
	}
	if err := m.Migrate(48); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("jev_model_changes") {
		t.Fatal("jev_model_changes missing after migration 48")
	}

	insert := func(actor string, before, after any) error {
		_, err := conn.Exec(`INSERT INTO jev_model_changes (at, actor, before_value, after_value) VALUES ('2026-10-22T00:00:00Z', ?, ?, ?)`, actor, before, after)
		return err
	}
	for name, err := range map[string]error{
		"model to model": insert("gui", "a", "b"),
		"unset to model": insert("gui", nil, "b"),
		"model to unset": insert("gui", "b", nil),
	} {
		if err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"unknown actor":         insert("mcp", "a", "b"),
		"unchanged model":       insert("gui", "a", "a"),
		"unset to unset":        insert("gui", nil, nil),
		"missing actor (empty)": insert("", "a", "b"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("jev_model_changes") {
		t.Fatal("jev_model_changes still exists after the down migration")
	}
	if !exists("secrets") {
		t.Fatal("the down migration dropped secrets")
	}
}

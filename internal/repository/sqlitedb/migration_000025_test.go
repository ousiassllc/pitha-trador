package sqlitedb_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000025 adds jev_decisions_instrument_type_timestamp_idx (issue
// #498) and its down migration removes exactly that index.
func TestMigration000025_AddsAndDropsInstrumentTypeTimestampIndex(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db"))
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
	hasIndex := func() bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'jev_decisions_instrument_type_timestamp_idx'`).Scan(&n); err != nil {
			t.Fatalf("query index: %v", err)
		}
		return n == 1
	}

	if err := m.Migrate(24); err != nil {
		t.Fatalf("migrate to 24: %v", err)
	}
	if hasIndex() {
		t.Fatalf("index exists before migration 25")
	}
	if err := m.Migrate(25); err != nil {
		t.Fatalf("migrate to 25: %v", err)
	}
	if !hasIndex() {
		t.Fatalf("index missing after migration 25")
	}
	if err := m.Migrate(24); err != nil {
		t.Fatalf("migrate down to 24: %v", err)
	}
	if hasIndex() {
		t.Fatalf("index still present after down migration")
	}
}

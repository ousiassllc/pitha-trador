package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migrations 000030 (daily_bars / daily_bar_runs) and 000031 (watch_lists /
// watch_list_entries) must be reversible: every up has a down that drops
// exactly its own tables, and the pair can be re-applied afterwards.
func TestMigrations000030And000031_AreReversible(t *testing.T) {
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
	tables := []string{"daily_bars", "daily_bar_runs", "watch_lists", "watch_list_entries"}
	count := func() int {
		n := 0
		for _, name := range tables {
			var c int
			if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&c); err != nil {
				t.Fatalf("sqlite_master %s: %v", name, err)
			}
			n += c
		}
		return n
	}

	if err := m.Migrate(31); err != nil {
		t.Fatalf("migrate to 31: %v", err)
	}
	if got := count(); got != len(tables) {
		t.Fatalf("tables after migration 31 = %d, want %d", got, len(tables))
	}
	if err := m.Migrate(30); err != nil {
		t.Fatalf("migrate down to 30: %v", err)
	}
	if got := count(); got != 2 {
		t.Fatalf("tables after down to 30 = %d, want 2 (daily_bars, daily_bar_runs)", got)
	}
	if err := m.Migrate(29); err != nil {
		t.Fatalf("migrate down to 29: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("tables after down to 29 = %d, want 0", got)
	}
	if err := m.Migrate(31); err != nil {
		t.Fatalf("re-migrate to 31: %v", err)
	}
	if got := count(); got != len(tables) {
		t.Fatalf("tables after re-migrate = %d, want %d", got, len(tables))
	}
}

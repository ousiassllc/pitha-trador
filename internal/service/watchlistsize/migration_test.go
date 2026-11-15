package watchlistsize_test

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

// Migration 000056 (issue #920) creates watch_list_runtime_days: one row per
// (trading day, source) holding counts only, with no symbol column, and one
// down step drops only it. It goes 55 -> 56 and steps one back, so it does not
// depend on what the migrations after it add.
func TestMigration000056_CreatesTheRuntimeWatchListDays(t *testing.T) {
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

	if err := m.Migrate(55); err != nil {
		t.Fatalf("migrate to 55: %v", err)
	}
	if exists("watch_list_runtime_days") {
		t.Fatal("watch_list_runtime_days exists before migration 56")
	}
	if err := m.Migrate(56); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("watch_list_runtime_days") {
		t.Fatal("watch_list_runtime_days missing after migration 56")
	}

	rows, err := conn.Query(`SELECT name FROM pragma_table_info('watch_list_runtime_days')`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == "symbol" || name == "symbols" || name == "code" {
			t.Errorf("column %q would store a symbol; the table keeps counts only", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	_ = rows.Close()

	insert := func(date, source string, entries, peak, samples int) error {
		_, err := conn.Exec(`INSERT INTO watch_list_runtime_days (session_date, source, entries, peak_entries, samples, recorded_at) VALUES (?, ?, ?, ?, ?, 't')`,
			date, source, entries, peak, samples)
		return err
	}
	if err := insert("2026-10-21", "ranking", 0, 0, 1); err != nil {
		t.Errorf("a valid row was refused: %v", err)
	}
	for name, err := range map[string]error{
		"a second row for one (date, source)": insert("2026-10-21", "ranking", 1, 1, 1),
		"a source outside the fixed set":      insert("2026-10-22", "daily_screen", 1, 1, 1),
		"a malformed date":                    insert("21/10/2026", "ranking", 1, 1, 1),
		"a negative size":                     insert("2026-10-22", "ranking", -1, 0, 1),
		"a peak below the latest size":        insert("2026-10-22", "ranking", 5, 4, 1),
		"zero samples":                        insert("2026-10-22", "ranking", 1, 1, 0),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("watch_list_runtime_days") {
		t.Fatal("watch_list_runtime_days still exists after the down migration")
	}
	if !exists("watch_lists") {
		t.Fatal("the down migration dropped watch_lists")
	}
}

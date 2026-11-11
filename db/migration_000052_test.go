package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000052 (issue #1054) creates session_diagnostics_daily: one row per
// trading day with a gap kind of the fixed set, no price column, and one down
// step drops only it. It goes 49 -> 52 and steps one back, so it does not
// depend on what 50 and 51 (other changes) add.
func TestMigration000052_CreatesTheDailySessionDiagnostics(t *testing.T) {
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

	if err := m.Migrate(49); err != nil {
		t.Fatalf("migrate to 49: %v", err)
	}
	if exists("session_diagnostics_daily") {
		t.Fatal("session_diagnostics_daily exists before migration 52")
	}
	if err := m.Migrate(52); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("session_diagnostics_daily") {
		t.Fatal("session_diagnostics_daily missing after migration 52")
	}

	insert := func(date, kind string) error {
		_, err := conn.Exec(`INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES (?, ?, 't')`, date, kind)
		return err
	}
	for i, kind := range []string{"none", "whole_day", "intraday", "insufficient_history", "in_progress"} {
		if err := insert(fmt.Sprintf("2026-10-%02d", 13+i), kind); err != nil {
			t.Errorf("gap_kind %q was refused: %v", kind, err)
		}
	}
	if err := insert("2026-10-20", "partial"); err == nil {
		t.Error("a gap_kind outside the fixed set was accepted")
	}
	if err := insert("2026-10-13", "none"); err == nil {
		t.Error("a second row for one session_date was accepted")
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("session_diagnostics_daily") {
		t.Fatal("session_diagnostics_daily still exists after the down migration")
	}
	if !exists("health_alerts") {
		t.Fatal("the down migration dropped health_alerts")
	}
}

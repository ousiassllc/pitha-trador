package migrationtests

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

// Migration 000059 creates research_tachibana_status, research_daily_bar_runs
// and research_daily_bar_progress (issue #1082): the state and the record of the
// 立花 research daily-bar collection. It touches no existing table (daily_bars
// and daily_bar_runs stay as they are). It migrates 58 -> 59 over a database
// that already holds daily bars, and steps one back.
func TestMigration000059_CreatesTheResearchBarTablesWithoutTouchingOthers(t *testing.T) {
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
	exec := func(stmt string, args ...any) error {
		_, err := conn.Exec(stmt, args...)
		return err
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	exists := func(name string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}
	tables := []string{"research_tachibana_status", "research_daily_bar_runs", "research_daily_bar_progress"}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate to 58: %v", err)
	}
	for _, name := range tables {
		if exists(name) {
			t.Fatalf("%s exists before migration 59", name)
		}
	}
	if err := exec(`INSERT INTO daily_bars (symbol, trade_date, open, high, low, close, volume, adj_open, adj_high, adj_low, adj_close, adj_volume)
		VALUES ('7203', '2026-10-14', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)`); err != nil {
		t.Fatalf("seed a daily bar: %v", err)
	}
	if err := exec(`INSERT INTO daily_bar_runs (run_date, status, started_at) VALUES ('2026-10-14', 'succeeded', 't')`); err != nil {
		t.Fatalf("seed a nightly run: %v", err)
	}

	if err := m.Migrate(59); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range append(tables, "research_daily_bar_progress_night_idx") {
		if !exists(name) {
			t.Fatalf("%s missing after migration 59", name)
		}
	}
	if count("daily_bars") != 1 || count("daily_bar_runs") != 1 {
		t.Fatal("the migration touched daily_bars or daily_bar_runs")
	}

	status := func(component, state string) error {
		return exec(`INSERT INTO research_tachibana_status (component, state, updated_at) VALUES (?, ?, 't')`, component, state)
	}
	for _, state := range []string{"disabled", "not_configured", "broker_is_tachibana", "waiting", "running", "ok", "error"} {
		if err := exec(`DELETE FROM research_tachibana_status`); err != nil {
			t.Fatal(err)
		}
		if err := status("bars", state); err != nil {
			t.Errorf("state %q rejected: %v", state, err)
		}
	}
	if err := status("bars", "ok"); err == nil {
		t.Error("a second row for one component was accepted")
	}
	if err := status("orders", "ok"); err == nil {
		t.Error("an unknown component was accepted")
	}
	if err := status("quotes", "sleeping"); err == nil {
		t.Error("an unknown state was accepted")
	}

	runRow := func(date, state string) error {
		return exec(`INSERT INTO research_daily_bar_runs (run_date, status, started_at) VALUES (?, ?, 't')`, date, state)
	}
	if err := runRow("2026-10-14", "running"); err != nil {
		t.Fatalf("a running research run: %v", err)
	}
	if err := runRow("2026-10-14", "failed"); err == nil {
		t.Error("a second run for one night was accepted")
	}
	if err := runRow("2026-10-15", "paused"); err == nil {
		t.Error("an unknown run status was accepted")
	}
	if err := runRow("20261015", "failed"); err == nil {
		t.Error("a run_date that is not YYYY-MM-DD was accepted")
	}

	progress := func(symbol, night string, ok int, first any) error {
		return exec(`INSERT INTO research_daily_bar_progress (symbol, last_night, last_ok, first_ok_night, updated_at)
			VALUES (?, ?, ?, ?, 't')`, symbol, night, ok, first)
	}
	if err := progress("7203", "2026-10-14", 0, nil); err != nil {
		t.Fatalf("a never-answered symbol (first_ok_night NULL): %v", err)
	}
	if err := progress("8001", "2026-10-14", 1, "2026-10-14"); err != nil {
		t.Fatalf("a backfilled symbol: %v", err)
	}
	if err := progress("7203", "2026-10-15", 1, nil); err == nil {
		t.Error("a second progress row for one symbol was accepted")
	}
	if err := progress("6758", "2026-10-14", 2, nil); err == nil {
		t.Error("last_ok other than 0 / 1 was accepted")
	}
	if err := progress("6758", "tonight", 1, nil); err == nil {
		t.Error("a last_night that is not YYYY-MM-DD was accepted")
	}
	if err := progress("6758", "2026-10-14", 1, "yesterday"); err == nil {
		t.Error("a first_ok_night that is not YYYY-MM-DD was accepted")
	}
	// The progress rows do not depend on daily_bars: a symbol without bars is fine, and purging bars leaves them.
	if err := exec(`DELETE FROM daily_bars`); err != nil {
		t.Fatalf("purging daily bars: %v", err)
	}
	if got := count("research_daily_bar_progress"); got != 2 {
		t.Fatalf("progress rows after the daily_bars purge = %d, want 2", got)
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, name := range tables {
		if exists(name) {
			t.Fatalf("%s remains after the down step", name)
		}
	}
	if got := count("daily_bar_runs"); got != 1 {
		t.Fatalf("daily_bar_runs rows after the down step = %d", got)
	}
}

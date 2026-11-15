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

// Migration 000058 creates virtual_paper_profiles, virtual_paper_days and
// virtual_paper_trades (issue #1087): the only foreign keys are to the profile
// table (nothing to market_snapshots / jev_decisions, which retention purges),
// a name is unique, a trade is unique per (profile, instrument, entry time), and
// no existing table is touched. It steps one back.
func TestMigration000058_CreatesTheVirtualPaperTables(t *testing.T) {
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
	exists := func(name string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}
	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate to 57: %v", err)
	}
	if exists("virtual_paper_profiles") {
		t.Fatal("the virtual Paper tables exist before migration 58")
	}
	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"virtual_paper_profiles", "virtual_paper_days", "virtual_paper_trades", "virtual_paper_profiles_status_idx", "virtual_paper_trades_profile_day_idx"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 58", name)
		}
	}

	const profile = `INSERT INTO virtual_paper_profiles (name, params_json, created_at, first_day, actor) VALUES (?, '{}', 't', '2026-10-19', 'mcp')`
	if err := exec(profile, "p"); err != nil {
		t.Fatalf("profile: %v", err)
	}
	trade := func(profileID int64, entryAt string, price float64) error {
		return exec(`INSERT INTO virtual_paper_trades (profile_id, day, instrument_id, symbol, direction, entry_at, entry_price, exit_at, exit_price, exit_reason, quantity,
			gross_return_pct, slippage_return_pct, fee_return_pct, net_return_pct, pnl_jpy, created_at)
			VALUES (?, '2026-10-19', 1, '7203', 'LONG', ?, ?, 'x', 1, 'take_profit', 1, 0, 0, 0, 0, 0, 't')`, profileID, entryAt, price)
	}
	if err := trade(1, "a", 1000); err != nil {
		t.Fatalf("trade: %v", err)
	}
	for name, err := range map[string]error{
		"a duplicate profile name":             exec(profile, "p"),
		"a 41-character name":                  exec(profile, "12345678901234567890123456789012345678901"),
		"a stopped profile without stopped_at": exec(`INSERT INTO virtual_paper_profiles (name, params_json, status, created_at, first_day, actor) VALUES ('s', '{}', 'stopped', 't', '2026-10-19', 'mcp')`),
		"another actor":                        exec(`INSERT INTO virtual_paper_profiles (name, params_json, created_at, first_day, actor) VALUES ('g', '{}', 't', '2026-10-19', 'gui')`),
		"a duplicate trade":                    trade(1, "a", 1000),
		"a non-positive entry price":           trade(1, "b", 0),
		"a trade of an unknown profile":        trade(99, "c", 1000),
		"a day of an unknown profile":          exec(`INSERT INTO virtual_paper_days (profile_id, day, status, completed_at) VALUES (99, '2026-10-19', 'done', 't')`),
		"an unknown day status":                exec(`INSERT INTO virtual_paper_days (profile_id, day, status, completed_at) VALUES (1, '2026-10-19', 'odd', 't')`),
		"more unfinished than trades":          exec(`INSERT INTO virtual_paper_days (profile_id, day, status, trades, unfinished, completed_at) VALUES (1, '2026-10-19', 'done', 1, 2, 't')`),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("virtual_paper_profiles") || exists("virtual_paper_days") || exists("virtual_paper_trades") {
		t.Error("the virtual Paper tables remain after the down migration")
	}
}

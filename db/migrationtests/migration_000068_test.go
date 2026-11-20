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

// Migration 000068 creates research_fx_candles and research_fx_days (issue #1084):
// one merged BID/ASK row per (pair, minute) where a side without a candle keeps
// NULLs, and a per-trading-day marker whose 'empty' status (a day GMO answered
// with no candle) must carry candle_count 0. It touches no existing table and
// steps back cleanly.
func TestMigration000068_CreatesTheResearchFxTablesWithoutTouchingOthers(t *testing.T) {
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
	count := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	if err := m.Migrate(67); err != nil {
		t.Fatalf("migrate to 67: %v", err)
	}
	if exists("research_fx_candles") || exists("research_fx_days") {
		t.Fatal("the FX tables exist before migration 68")
	}
	if err := exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	existing := []string{"instruments", "market_snapshots", "jev_decisions", "research_horizon_labels", "research_label_days"}
	before := map[string]int{}
	for _, tbl := range existing {
		before[tbl] = count(tbl)
	}

	if err := m.Migrate(68); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"research_fx_candles", "research_fx_days", "research_fx_candles_day_idx"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 68", name)
		}
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d: the migration must not touch existing tables", tbl, before[tbl], got)
		}
	}

	const at = "2026-10-14T21:00:00.000000000Z"
	insert := func(symbol, openTime string, bid, ask any) error {
		return exec(`INSERT INTO research_fx_candles (symbol, open_time, trading_day, bid_open, bid_high, bid_low, bid_close, ask_open, ask_high, ask_low, ask_close, recorded_at)
			VALUES (?, ?, '2026-10-15', ?, ?, ?, ?, ?, ?, ?, ?, 't')`, symbol, openTime, bid, bid, bid, bid, ask, ask, ask, ask)
	}
	if err := insert("USD_JPY", at, 149.8, 149.9); err != nil {
		t.Fatalf("a merged row: %v", err)
	}
	if err := insert("USD_JPY", "2026-10-14T21:01:00.000000000Z", 149.8, nil); err != nil {
		t.Fatalf("a BID-only row: %v", err)
	}
	if err := insert("USD_JPY", "2026-10-14T21:02:00.000000000Z", nil, 149.9); err != nil {
		t.Fatalf("an ASK-only row: %v", err)
	}
	// The same pair may repeat a minute for another pair.
	if err := insert("EUR_USD", at, 1.1, 1.2); err != nil {
		t.Fatalf("another pair at the same minute: %v", err)
	}
	for name, err := range map[string]error{
		"a duplicate (pair, minute)": insert("USD_JPY", at, 1.0, 1.0),
		"neither side":               insert("USD_JPY", "2026-10-14T21:03:00.000000000Z", nil, nil),
		"a non-positive price":       insert("USD_JPY", "2026-10-14T21:04:00.000000000Z", 0, 1.0),
		"a pair that is not a pair":  insert("usd/jpy", "2026-10-14T21:05:00.000000000Z", 1.0, 1.0),
		"a half-filled side": exec(`INSERT INTO research_fx_candles (symbol, open_time, trading_day, bid_open, bid_high, ask_open, ask_high, ask_low, ask_close, recorded_at)
			VALUES ('USD_JPY', '2026-10-14T21:06:00.000000000Z', '2026-10-15', 1, 1, 1, 1, 1, 1, 't')`),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	const day = `INSERT INTO research_fx_days (symbol, trading_day, status, candle_count, fetched_at) VALUES ('USD_JPY', ?, ?, ?, 't')`
	if err := exec(day, "2026-10-15", "recorded", 3); err != nil {
		t.Fatalf("a recorded day: %v", err)
	}
	if err := exec(day, "2026-10-17", "empty", 0); err != nil {
		t.Fatalf("an empty day: %v", err)
	}
	for name, err := range map[string]error{
		"a second marker for one day":    exec(day, "2026-10-15", "recorded", 3),
		"an unknown status":              exec(day, "2026-10-18", "missing", 0),
		"an empty day with candles":      exec(day, "2026-10-19", "empty", 2),
		"a recorded day without candles": exec(day, "2026-10-20", "recorded", 0),
		"a negative count":               exec(day, "2026-10-21", "recorded", -1),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Migrate(67); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("research_fx_candles") || exists("research_fx_days") || exists("research_fx_candles_day_idx") {
		t.Fatal("the FX tables remain after the down step")
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows after the down step = %d, want %d", tbl, got, before[tbl])
		}
	}
}

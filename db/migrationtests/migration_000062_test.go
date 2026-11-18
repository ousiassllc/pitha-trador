package migrationtests

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

// Migration 000062 creates the J-Quants research tables (issue #1093) and adds
// research_horizon_labels.exit_source. Every J-Quants table carries
// source = 'jquants' (nothing else is accepted), natural keys make a re-fetch an
// upsert, existing label rows read 'snapshot' afterwards and a row the recorder
// wrote keeps its numbers, and one down step removes the new tables and the column
// without touching the label rows. It migrates 61 -> 62 over a database that
// already holds snapshots and labels and steps one back.
func TestMigration000062_CreatesTheJQuantsTablesAndTheLabelProvenanceColumn(t *testing.T) {
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
	jquantsTables := []string{"research_jquants_symbols", "research_jquants_daily_bars", "research_jquants_minute_bars", "research_jquants_coverage", "research_jquants_fetch_log"}

	if err := m.Migrate(61); err != nil {
		t.Fatalf("migrate to 61: %v", err)
	}
	for _, tbl := range jquantsTables {
		if exists(tbl) {
			t.Fatalf("%s exists before migration 62", tbl)
		}
	}
	if err := exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps)
		VALUES (11, 1, '7203', '2026-10-14T01:00:00.000000000Z', '2026-10-14', 7, '30m', '2026-10-14T01:30:00.000000000Z', 1000, 1010, 1, 100.0, 15.0, 85.0)`); err != nil {
		t.Fatalf("seed an existing label: %v", err)
	}

	if err := m.Migrate(62); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range append(jquantsTables, "research_jquants_symbols_symbol_idx", "research_jquants_daily_bars_symbol_idx", "research_jquants_minute_bars_day_symbol_idx", "research_horizon_labels_jquants_idx") {
		if !exists(name) {
			t.Fatalf("%s missing after migration 62", name)
		}
	}
	var source string
	var net float64
	if err := conn.QueryRow(`SELECT exit_source, long_net_bps FROM research_horizon_labels WHERE snapshot_id = 11`).Scan(&source, &net); err != nil || source != "snapshot" || net != 85.0 {
		t.Fatalf("the existing label = (%q, %v, %v), want it kept with exit_source 'snapshot'", source, net, err)
	}
	if err := exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps, exit_source)
		VALUES (12, 1, '7203', 't', '2026-10-14', 7, '60m', 't', 1, 1, 1, 1.0, 1.0, 0.0, 'jquants')`); err != nil {
		t.Fatalf("a label with a J-Quants exit: %v", err)
	}
	if err := exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps, exit_source)
		VALUES (13, 1, '7203', 't', '2026-10-14', 7, '60m', 't', 1, 1, 1, 1.0, 1.0, 0.0, 'other')`); err == nil {
		t.Error("an unknown exit_source was accepted")
	}

	for name, stmt := range map[string]string{
		"symbols":     `INSERT INTO research_jquants_symbols (source, as_of, code, symbol, fetched_at) VALUES ('other', '2026-10-14', '72030', '7203', 't')`,
		"daily bars":  `INSERT INTO research_jquants_daily_bars (source, day, code, symbol, fetched_at) VALUES ('other', '2026-10-14', '72030', '7203', 't')`,
		"minute bars": `INSERT INTO research_jquants_minute_bars (source, code, symbol, day, bar_at, open, high, low, close, volume, turnover, fetched_at) VALUES ('other', '72030', '7203', '2026-10-14', 't', 1, 1, 1, 1, 1, 1, 't')`,
		"coverage":    `INSERT INTO research_jquants_coverage (source, dataset, day, row_count, completed_at) VALUES ('other', 'daily', '2026-10-14', 1, 't')`,
		"fetch log":   `INSERT INTO research_jquants_fetch_log (source, day, dataset) VALUES ('other', '2026-10-14', 'daily')`,
	} {
		if err := exec(stmt); err == nil {
			t.Errorf("a %s row with source 'other' was accepted", name)
		}
	}
	const daily = `INSERT INTO research_jquants_daily_bars (day, code, symbol, open, fetched_at) VALUES ('2026-10-14', '72030', '7203', 1000, 't')`
	if err := exec(daily); err != nil {
		t.Fatalf("daily bar: %v", err)
	}
	if err := exec(daily); err == nil {
		t.Error("a second daily bar for one (day, code) was accepted")
	}
	if err := exec(`INSERT INTO research_jquants_daily_bars (day, code, symbol, fetched_at) VALUES ('2026-10-15', '72030', '7203', 't')`); err != nil {
		t.Errorf("a no-trade daily bar (null prices) was refused: %v", err)
	}
	const minute = `INSERT INTO research_jquants_minute_bars (code, symbol, day, bar_at, open, high, low, close, volume, turnover, fetched_at)
		VALUES ('72030', '7203', '2026-10-14', '2026-10-14T00:00:00.000000000Z', 1000, 1001, 999, 1000, 100, 100000, 't')`
	if err := exec(minute); err != nil {
		t.Fatalf("minute bar: %v", err)
	}
	if err := exec(minute); err == nil {
		t.Error("a second minute bar for one (code, bar_at) was accepted")
	}
	if err := exec(`INSERT INTO research_jquants_minute_bars (code, symbol, day, bar_at, open, high, low, close, volume, turnover, fetched_at)
		VALUES ('72030', '7203', '2026-10-14', 'x', 0, 1, 1, 1, 1, 1, 't')`); err == nil {
		t.Error("a minute bar with a non-positive price was accepted")
	}
	if err := exec(`INSERT INTO research_jquants_coverage (dataset, day, row_count, completed_at) VALUES ('ticks', '2026-10-14', 1, 't')`); err == nil {
		t.Error("an unknown coverage dataset was accepted")
	}
	if err := exec(`INSERT INTO research_jquants_fetch_log (day, dataset) VALUES ('2026-10-14', 'relabel')`); err == nil {
		t.Error("an unknown fetch-log dataset was accepted")
	}

	if err := m.Migrate(61); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, tbl := range jquantsTables {
		if exists(tbl) {
			t.Fatalf("%s remains after the down step", tbl)
		}
	}
	if got := count("research_horizon_labels"); got != 2 {
		t.Fatalf("label rows after the down step = %d, want the 2 kept", got)
	}
	if err := conn.QueryRow(`SELECT exit_source FROM research_horizon_labels`).Scan(&source); err == nil {
		t.Error("exit_source still exists after the down step")
	}
}

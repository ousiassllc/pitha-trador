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

// Migration 000055 creates research_horizon_labels, research_label_days and
// research_label_day_stats (issue #1074): one row per (snapshot, horizon) with no foreign key to
// market_snapshots (retention purges those after 90 days), nullable per-side
// results (a side with no executable quote has all three columns NULL), and a
// per-JST-day completion mark. It touches no existing table. It migrates 54 ->
// 55 over a database that already holds snapshots and decisions, and steps one
// back.
func TestMigration000055_CreatesTheResearchLabelTablesWithoutTouchingOthers(t *testing.T) {
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

	if err := m.Migrate(54); err != nil {
		t.Fatalf("migrate to 54: %v", err)
	}
	if exists("research_horizon_labels") || exists("research_label_days") || exists("research_label_day_stats") {
		t.Fatal("the research tables exist before migration 55")
	}
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`,
		`INSERT INTO market_snapshots (id, instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
			VALUES (11, 1, '7203', '2026-10-14T01:00:00.000000000Z', 1000, 1, 1, 1, 1, '{}')`,
		`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
			VALUES (1, 1, '7203', '2026-10-14T01:00:00.000000000Z', 'trader', 'h', '{}', 'q', '{}', 1, 'm')`,
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	existing := []string{"instruments", "market_snapshots", "jev_decisions", "calibration_outcomes", "counterfactual_labels"}
	before := map[string]int{}
	for _, tbl := range existing {
		before[tbl] = count(tbl)
	}

	if err := m.Migrate(55); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"research_horizon_labels", "research_label_days", "research_label_day_stats", "research_horizon_labels_day_sample_idx", "research_horizon_labels_instrument_at_idx"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 55", name)
		}
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d: the migration must not touch existing tables", tbl, before[tbl], got)
		}
	}

	insert := func(snapshotID int, horizon string, long, short any) error {
		return exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version,
			long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps)
			VALUES (?, 1, '7203', '2026-10-14T01:00:00.000000000Z', '2026-10-14', 7, ?, '2026-10-14T01:30:00.000000000Z', 1000, 1010, 1,
			?, ?, ?, ?, ?, ?)`, snapshotID, horizon, long, long, long, short, short, short)
	}
	// The snapshot does not need to exist: no foreign key (retention purges snapshots).
	if err := insert(999999, "30m", 5.0, nil); err != nil {
		t.Fatalf("a LONG-only row for a snapshot that is gone: %v", err)
	}
	if err := insert(11, "close", nil, -3.0); err != nil {
		t.Fatalf("a SHORT-only row: %v", err)
	}
	for name, err := range map[string]error{
		"a duplicate (snapshot, horizon)": insert(999999, "30m", 1.0, 1.0),
		"an unknown horizon":              insert(11, "45m", 1.0, 1.0),
		"neither side":                    insert(12, "60m", nil, nil),
		"a half-filled side": exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps)
			VALUES (13, 1, '7203', 't', '2026-10-14', 7, '60m', 't', 1, 1, 1, 1.0)`),
		"a non-positive entry price": exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps)
			VALUES (14, 1, '7203', 't', '2026-10-14', 7, '60m', 't', 0, 1, 1, 1.0, 1.0, 0.0)`),
		"another label version": exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps)
			VALUES (15, 1, '7203', 't', '2026-10-14', 7, '60m', 't', 1, 1, 2, 1.0, 1.0, 0.0)`),
		"a sample bucket of 100": exec(`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps)
			VALUES (16, 1, '7203', 't', '2026-10-14', 100, '60m', 't', 1, 1, 1, 1.0, 1.0, 0.0)`),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	const mark = `INSERT INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at) VALUES ('2026-10-14', 3, 2, 1, 1, 't')`
	if err := exec(mark); err != nil {
		t.Fatalf("day mark: %v", err)
	}
	if err := exec(mark); err == nil {
		t.Error("a second mark for one day was accepted")
	}
	const stat = `INSERT INTO research_label_day_stats (day, horizon, side, row_count, sum_gross, sum_net, sum_cost, wins, spread_rows, sum_spread, spread_above_cost, sum_gross_less_half_spread)
		VALUES ('2026-10-14', '30m', ?, 2, 10, 0, 20, 1, 2, 16, 1, 2)`
	if err := exec(stat, "LONG"); err != nil {
		t.Fatalf("day stats: %v", err)
	}
	if err := exec(stat, "LONG"); err == nil {
		t.Error("a second sums row for one (day, horizon, side) was accepted")
	}
	if err := exec(stat, "FLAT"); err == nil {
		t.Error("an unknown side was accepted")
	}
	if err := exec(`INSERT INTO research_label_day_stats (day, horizon, side, row_count, sum_gross, sum_net, sum_cost, wins, spread_rows, sum_spread, spread_above_cost, sum_gross_less_half_spread)
		VALUES ('2026-10-14', '60m', 'LONG', 2, 0, 0, 0, 3, 0, 0, 0, 0)`); err == nil {
		t.Error("more wins than rows was accepted")
	}
	// A purged snapshot does not block or cascade.
	if err := exec(`DELETE FROM market_snapshots WHERE id = 11`); err != nil {
		t.Fatalf("purging a labeled snapshot: %v", err)
	}
	if got := count("research_horizon_labels"); got != 2 {
		t.Fatalf("research rows after the snapshot purge = %d, want 2", got)
	}

	if err := m.Migrate(54); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("research_horizon_labels") || exists("research_label_days") || exists("research_label_day_stats") {
		t.Fatal("the research tables remain after the down step")
	}
	if got := count("jev_decisions"); got != before["jev_decisions"] {
		t.Fatalf("jev_decisions rows after the down step = %d", got)
	}
}

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

// Migration 000050 (issue #1052) adds the counterfactual_labels table, the
// calibration_outcome_origins view and a trade_signals.jev_decision_id index. The
// table holds triple-barrier labels of LONG/SHORT decisions only (a label needs
// a barrier, a net return and an entry price, version 2, one per decision and
// horizon); calibration_outcomes keeps its rows untouched; one down step removes
// the three objects and nothing else.
func TestMigration000050_CreatesTheCounterfactualLabelTableAndOriginsView(t *testing.T) {
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
	exists := func(kind, name string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, kind, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}
	exec := func(stmt string, args ...any) error {
		_, err := conn.Exec(stmt, args...)
		return err
	}

	if err := m.Migrate(49); err != nil {
		t.Fatalf("migrate to 49: %v", err)
	}
	if exists("table", "counterfactual_labels") || exists("view", "calibration_outcome_origins") {
		t.Fatal("the counterfactual objects exist before migration 50")
	}
	if err := m.Migrate(50); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("table", "counterfactual_labels") || !exists("view", "calibration_outcome_origins") || !exists("index", "trade_signals_jev_decision_idx") {
		t.Fatal("migration 50 did not create the table, the view and the index")
	}

	if err := exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`); err != nil {
		t.Fatalf("instrument: %v", err)
	}
	if err := exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		VALUES (1, 1, '7203', '2026-10-01T01:00:00.000000000Z', 'trader', 'h', '{}', 'v', '{}', 'LONG', 0.7, 1, 'm', 't')`); err != nil {
		t.Fatalf("decision: %v", err)
	}
	insert := func(horizon int, hit string, version int, ref float64) error {
		return exec(`INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, barrier_hit, net_return_bps, entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff)
			VALUES (1, ?, ?, 1.5, ?, ?, 0, 0, 0, '2026-09-20')`, horizon, hit, ref, version)
	}
	if err := insert(15, "tp", 2, 1000); err != nil {
		t.Fatalf("a valid label: %v", err)
	}
	for name, err := range map[string]error{
		"a repeated (decision, horizon) pair": insert(15, "sl", 2, 1000),
		"an unknown barrier":                  insert(10, "flat", 2, 1000),
		"the close-only label version":        insert(10, "tp", 1, 1000),
		"a zero entry price":                  insert(10, "tp", 2, 0),
		"a zero horizon":                      insert(0, "tp", 2, 1000),
	} {
		if err == nil {
			t.Errorf("counterfactual_labels accepts %s", name)
		}
	}
	if err := exec(`INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, barrier_hit, net_return_bps, entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff)
		VALUES (999, 15, 'tp', 1, 1000, 2, 0, 0, 0, 'x')`); err == nil {
		t.Error("counterfactual_labels accepts a label of a decision that does not exist")
	}

	if err := m.Migrate(49); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("table", "counterfactual_labels") || exists("view", "calibration_outcome_origins") || exists("index", "trade_signals_jev_decision_idx") {
		t.Fatal("the down step left the counterfactual objects behind")
	}
	if !exists("table", "calibration_outcomes") || !exists("table", "trade_signals") || !exists("table", "disclosure_items") {
		t.Fatal("the down step removed more than its own objects")
	}
}

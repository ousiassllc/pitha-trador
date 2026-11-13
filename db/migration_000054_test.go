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

// Migration 000054 creates jev_trial_decisions (issue #1059): a row per (live
// decision, arm) for a successful, refused, partial or failed call; answers are
// NULL when refused, cost_source is one of three values (an unknown cost is never
// stored as 0), llm_cutoff_status defaults to 'unverified', and one down step drops
// the table and nothing else. It migrates 53 -> 54 and steps one back.
func TestMigration000054_CreatesTheTrialTableAndRoundTrips(t *testing.T) {
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

	if err := m.Migrate(53); err != nil {
		t.Fatalf("migrate to 53: %v", err)
	}
	if exists("jev_trial_decisions") {
		t.Fatal("jev_trial_decisions exists before migration 54")
	}
	if err := m.Migrate(54); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("jev_trial_decisions") {
		t.Fatal("jev_trial_decisions missing after migration 54")
	}
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}

	insert := func(decisionID int, arm, status, failureCode, costSource string) error {
		_, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, failure_code, cost_source,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability)
			VALUES (?, ?, 'trader-v3', 'm', ?, ?, ?, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5)`, decisionID, arm, status, failureCode, costSource)
		return err
	}
	if err := insert(1, "a", "ok", "", "reported"); err != nil {
		t.Fatalf("valid trial row: %v", err)
	}
	for name, err := range map[string]error{
		"duplicate (decision, arm)": insert(1, "a", "ok", "", "reported"),
		"unknown live decision":     insert(99, "b", "ok", "", "reported"),
		"bad status":                insert(1, "b", "done", "", "reported"),
		"bad cost source":           insert(1, "b", "ok", "", "guess"),
		"failed without a code":     insert(1, "b", "failed", "", "unknown"),
		"ok with a failure code":    insert(1, "b", "ok", "http_402", "reported"),
		"empty arm":                 insert(1, "", "ok", "", "reported"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	var cutoff string
	var rag int
	if err := conn.QueryRow(`SELECT llm_cutoff_status, rag_includes_live_model_answers FROM jev_trial_decisions WHERE arm_id = 'a'`).Scan(&cutoff, &rag); err != nil || cutoff != "unverified" || rag != 0 {
		t.Errorf("defaults = %q/%d (err %v), want unverified/0", cutoff, rag, err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("jev_trial_decisions") {
		t.Fatal("jev_trial_decisions still exists after the down migration")
	}
	if !exists("jev_decisions") || !exists("jev_shadow_decisions") {
		t.Fatal("the down migration dropped another table")
	}
}

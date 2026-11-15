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

// Migration 000057 creates jev_replay_runs and jev_replay_results (issue #1085):
// a run (spec, status, estimate, progress, stop flag) and one result row per
// (run, live decision, arm) with the trial row's answer/cost/status columns plus
// the cutoff status and the reference-only mark. An unverified cutoff must carry
// the reference-only mark, a duplicate (run, decision, arm) is refused, and one
// down step drops both tables and nothing else. It migrates 56 -> 57 and steps
// one back.
func TestMigration000057_CreatesTheReplayTablesAndRoundTrips(t *testing.T) {
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

	if err := m.Migrate(56); err != nil {
		t.Fatalf("migrate to 56: %v", err)
	}
	if exists("jev_replay_runs") || exists("jev_replay_results") {
		t.Fatal("a replay table exists before migration 57")
	}
	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("jev_replay_runs") || !exists("jev_replay_results") {
		t.Fatal("a replay table is missing after migration 57")
	}
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	run := func(id int, status string, finished any) error {
		_, err := conn.Exec(`INSERT INTO jev_replay_runs (id, status, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
			estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_token, created_at, finished_at, updated_at)
			VALUES (?, ?, '{}', 'a', 'b', 10, 1, '[]', 1, 1, 0.01, 'tok', 't', ?, 't')`, id, status, finished)
		return err
	}
	if err := run(1, "pending", nil); err != nil {
		t.Fatalf("valid run: %v", err)
	}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"bad status", run(2, "done", nil)},
		{"finished run without a time", run(3, "completed", nil)},
		{"pending run with a finish time", run(4, "pending", "t")},
		{"duplicate id", run(1, "pending", nil)},
	} {
		if c.err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	if _, err := conn.Exec(`INSERT INTO jev_replay_runs (id, status, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
		estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_token, created_at, updated_at)
		VALUES (9, 'pending', '{}', 'a', 'b', 1001, 1, '[]', 1, 1, 0.01, 'tok', 't', 't')`); err == nil {
		t.Error("max_rows above 1000 was accepted")
	}

	insert := func(runID, decisionID int, arm, status, failureCode, costSource, cutoff string, ref int) error {
		_, err := conn.Exec(`INSERT INTO jev_replay_results (run_id, jev_decision_id, arm_id, prompt_version, requested_model, status, failure_code, cost_source,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability, llm_cutoff_status, reference_only, created_at)
			VALUES (?, ?, ?, 'trader-v3', 'm', ?, ?, ?, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5, ?, ?, 't')`,
			runID, decisionID, arm, status, failureCode, costSource, cutoff, ref)
		return err
	}
	if err := insert(1, 1, "a", "ok", "", "reported", "verified", 0); err != nil {
		t.Fatalf("valid result row: %v", err)
	}
	if err := insert(1, 1, "b", "ok", "", "unknown", "unverified", 1); err != nil {
		t.Fatalf("valid unverified result row: %v", err)
	}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"duplicate (run, decision, arm)", insert(1, 1, "a", "ok", "", "reported", "verified", 0)},
		{"unknown run", insert(99, 1, "c", "ok", "", "reported", "verified", 0)},
		{"unknown live decision", insert(1, 99, "c", "ok", "", "reported", "verified", 0)},
		{"unverified without the mark", insert(1, 1, "c", "ok", "", "reported", "unverified", 0)},
		{"unknown cutoff status", insert(1, 1, "c", "ok", "", "reported", "confirmed", 1)},
		{"bad status", insert(1, 1, "c", "done", "", "reported", "verified", 0)},
		{"bad cost source", insert(1, 1, "c", "ok", "", "guess", "verified", 0)},
		{"failed without a code", insert(1, 1, "c", "failed", "", "unknown", "verified", 0)},
		{"ok with a failure code", insert(1, 1, "c", "ok", "http_402", "reported", "verified", 0)},
		{"empty arm", insert(1, 1, "", "ok", "", "reported", "verified", 0)},
	} {
		if c.err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	// The same decision and arm in another run is a different row.
	if err := run(5, "pending", nil); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if err := insert(5, 1, "a", "ok", "", "reported", "verified", 0); err != nil {
		t.Errorf("the same (decision, arm) in another run was refused: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("jev_replay_runs") || exists("jev_replay_results") {
		t.Fatal("a replay table still exists after the down migration")
	}
	if !exists("jev_decisions") || !exists("jev_trial_decisions") || !exists("jev_trial_skips") {
		t.Fatal("the down migration dropped another table")
	}
}

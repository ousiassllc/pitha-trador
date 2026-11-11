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

// Migration 000053 (issue #1052) adds counterfactual_labels.skip_kind and
// cutoff_unverified and splits the origin of calibration_outcome_origins. It
// goes 50 -> 53 over a database that already holds labels, so it checks that the
// existing rows get the kind their signal gives them, that calibration_outcomes
// is untouched, and that one down step restores the old view and the old table.
func TestMigration000053_SplitsTheSkipKindOfExistingLabels(t *testing.T) {
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
	exec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(stmt, args...); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	columns := func() map[string]bool {
		t.Helper()
		rows, err := conn.Query(`SELECT name FROM pragma_table_info('counterfactual_labels')`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		out := map[string]bool{}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			out[n] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	origins := func() map[int64]string {
		t.Helper()
		rows, err := conn.Query(`SELECT jev_decision_id, origin FROM calibration_outcome_origins`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		out := map[int64]string{}
		for rows.Next() {
			var id int64
			var o string
			if err := rows.Scan(&id, &o); err != nil {
				t.Fatal(err)
			}
			out[id] = o
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if err := m.Migrate(50); err != nil {
		t.Fatalf("migrate to 50: %v", err)
	}
	if columns()["skip_kind"] {
		t.Fatal("skip_kind exists before migration 53")
	}
	exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`)
	// Decisions 1..4: a guard skip, a Risk rejection, no signal, a signal with a later Risk rejection.
	for id := 1; id <= 4; id++ {
		exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			VALUES (?, 1, '7203', '2026-10-01T01:00:00.000000000Z', 'trader', 'h', '{}', 'v', '{}', 'LONG', 0.7, 1, 'm', 't')`, id)
		exec(`INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, reject_reason, barrier_hit, net_return_bps, entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff)
			VALUES (?, 15, '', 'tp', 1.5, 1000, 2, 0, 0, 0, '2026-09-20')`, id)
		exec(`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, net_return_bps, label_version) VALUES (?, 30, 0, 0, 0, 1.0, 2)`, id)
	}
	signal := func(decision int, reason string) {
		exec(`INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (1, ?, '7203', '2026-10-01T01:00:01.000000000Z', 'NONE', 'p', 0, ?)`, decision, reason)
	}
	signal(1, "entry_quality_below_threshold")
	signal(2, "risk_engine_rejected: bucket_budget_exceeded")
	signal(4, "entry_quality_below_threshold")
	signal(4, "risk_engine_rejected: kill_switch_active") // the latest signal decides
	exec(`UPDATE counterfactual_labels SET reject_reason = 'entry_quality_below_threshold' WHERE jev_decision_id = 1`)
	var outcomesBefore int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM calibration_outcomes`).Scan(&outcomesBefore); err != nil {
		t.Fatal(err)
	}

	if err := m.Migrate(53); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if c := columns(); !c["skip_kind"] || !c["cutoff_unverified"] {
		t.Fatalf("columns after migration 53 = %v, want skip_kind and cutoff_unverified", c)
	}
	want := map[int64]string{1: "guard_skipped", 2: "risk_rejected", 3: "no_signal", 4: "risk_rejected"}
	rows, err := conn.Query(`SELECT jev_decision_id, skip_kind, cutoff_unverified FROM counterfactual_labels`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		var kind string
		var unverified int
		if err := rows.Scan(&id, &kind, &unverified); err != nil {
			t.Fatal(err)
		}
		if kind != want[id] || unverified != 0 {
			t.Errorf("label of decision %d: skip_kind %q cutoff_unverified %d, want %q / 0", id, kind, unverified, want[id])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for id, o := range origins() {
		if o != want[id] {
			t.Errorf("origin of decision %d = %q, want %q (the view and the backfill are one rule)", id, o, want[id])
		}
	}
	var outcomesAfter int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM calibration_outcomes`).Scan(&outcomesAfter); err != nil || outcomesAfter != outcomesBefore {
		t.Fatalf("calibration_outcomes rows %d -> %d, %v: the migration must not touch them", outcomesBefore, outcomesAfter, err)
	}
	for name, stmt := range map[string]string{
		"an unknown skip kind": `UPDATE counterfactual_labels SET skip_kind = 'skipped' WHERE jev_decision_id = 1`,
		"a cutoff mark of 2":   `UPDATE counterfactual_labels SET cutoff_unverified = 2 WHERE jev_decision_id = 1`,
	} {
		if _, err := conn.Exec(stmt); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Migrate(52); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if c := columns(); c["skip_kind"] || c["cutoff_unverified"] {
		t.Fatalf("columns after the down step = %v, want neither", c)
	}
	for id, o := range origins() {
		if o != "skipped" {
			t.Errorf("origin of decision %d = %q after the down step, want the old 'skipped'", id, o)
		}
	}
}

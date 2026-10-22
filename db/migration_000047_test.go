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

// Migration 000047 creates jev_shadow_decisions and jev_prompt_promotions (issue
// #872): the shadow row is tied to a live jev_decisions row, one row per live
// decision and candidate version, its answers are CHECK-constrained like the live
// ones, a promotion's actor is 'mcp' or 'gui', and one down step drops both
// tables (and nothing else). It migrates 43 -> 47 and steps one back, so it
// does not depend on whether 44-46 (other changes) are present: golang-migrate
// walks the migrations that exist between the two versions.
func TestMigration000047_RoundTripsAndConstrainsTheShadowTables(t *testing.T) {
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

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}
	if exists("jev_shadow_decisions") || exists("jev_prompt_promotions") {
		t.Fatal("shadow tables exist before migration 47")
	}
	if err := m.Migrate(47); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("jev_shadow_decisions") || !exists("jev_prompt_promotions") {
		t.Fatal("shadow tables missing after migration 47")
	}

	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	insert := func(decisionID int, version, direction, regime, eq string, confidence, cost float64) error {
		_, err := conn.Exec(`INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
			confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost)
			VALUES (?, ?, 'm', '{}', ?, ?, ?, ?, 0.1, 0.1, 0.5, 1, 100, ?)`, decisionID, version, direction, regime, eq, confidence, cost)
		return err
	}
	if err := insert(1, "trader-v4", "LONG", "TREND", "good", 0.7, 0.0001); err != nil {
		t.Fatalf("valid shadow row: %v", err)
	}
	for name, err := range map[string]error{
		"duplicate (decision, version)": insert(1, "trader-v4", "LONG", "TREND", "good", 0.7, 0.0001),
		"unknown live decision (FK)":    insert(99, "trader-v4", "LONG", "TREND", "good", 0.7, 0.0001),
		"bad direction":                 insert(1, "trader-v5", "UP", "TREND", "good", 0.7, 0.0001),
		"bad regime":                    insert(1, "trader-v5", "LONG", "SIDEWAYS", "good", 0.7, 0.0001),
		"bad entry_quality":             insert(1, "trader-v5", "LONG", "TREND", "great", 0.7, 0.0001),
		"confidence above 1":            insert(1, "trader-v5", "LONG", "TREND", "good", 1.5, 0.0001),
		"negative cost":                 insert(1, "trader-v5", "LONG", "TREND", "good", 0.7, -1),
		"empty version":                 insert(1, "", "LONG", "TREND", "good", 0.7, 0.0001),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A second candidate version for the same live decision is allowed.
	if err := insert(1, "trader-v5", "SHORT", "RANGE", "poor", 0.5, 0.0002); err != nil {
		t.Errorf("a second candidate version for one decision was refused: %v", err)
	}
	// cost_is_estimate marks a row whose token count was estimated (no usage
	// in the response); it defaults to 0 (measured) and only takes 0 or 1.
	var estimate int
	if err := conn.QueryRow(`SELECT cost_is_estimate FROM jev_shadow_decisions WHERE prompt_version = 'trader-v4'`).Scan(&estimate); err != nil || estimate != 0 {
		t.Errorf("cost_is_estimate default = %d (err %v), want 0", estimate, err)
	}
	setEstimate := func(v int) error {
		_, err := conn.Exec(`UPDATE jev_shadow_decisions SET cost_is_estimate = ? WHERE prompt_version = 'trader-v4'`, v)
		return err
	}
	if err := setEstimate(1); err != nil {
		t.Errorf("cost_is_estimate = 1 was refused: %v", err)
	}
	if err := setEstimate(2); err == nil {
		t.Error("cost_is_estimate = 2 was accepted: only 0 and 1 exist")
	}

	promote := func(actor string) error {
		_, err := conn.Exec(`INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES ('t', ?, '', 'a', 'b')`, actor)
		return err
	}
	if err := promote("gui"); err != nil {
		t.Errorf("gui promotion: %v", err)
	}
	if err := promote("mcp"); err != nil {
		t.Errorf("mcp promotion: %v", err)
	}
	if err := promote("selfimprove"); err == nil {
		t.Error("a promotion by the self-improvement loop was accepted: only 'mcp' and 'gui' exist")
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("jev_shadow_decisions") || exists("jev_prompt_promotions") {
		t.Fatal("shadow tables still exist after the down migration")
	}
	if !exists("jev_decisions") {
		t.Fatal("the down migration dropped jev_decisions")
	}
}

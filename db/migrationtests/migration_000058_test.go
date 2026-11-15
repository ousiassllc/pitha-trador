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

// Migration 000058 lets jev_trial_decisions.cost_source be 'subscription' (issue
// #1081: the ChatGPT subscription through the Codex CLI). SQLite cannot alter a
// CHECK, and a table rebuild does not fit the directory's line limit, so the
// migration edits the CHECK text with SQLite's documented writable_schema
// procedure: the rows of 57 are untouched, the stored schema stays sound
// (integrity_check, a reopened connection), a subscription row must have no USD
// cost and count 0, and one down step restores the old CHECK and drops only the
// subscription rows. It migrates 57 -> 58 and steps one back.
func TestMigration000058_AllowsTheSubscriptionCostSourceAndKeepsTheRows(t *testing.T) {
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
	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate to 57: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`,
		`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
			VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm'), (2, 1, '7203', 't', 'trader', 'h2', '{}', 'trader-v3', '{}', 1, 'm')`,
		`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, provider, requested_model, status, cost_source, cost_usd, counted_cost_usd,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability, input_tokens, created_at)
			VALUES (1, 'old', 'trader-v3', 'openrouter', 'm', 'ok', 'reported', 0.001, 0.001, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5, 100, '2026-11-14T00:00:00.000Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	if _, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, cost_source,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability)
			VALUES (2, 'sub', 'trader-v3', 'm', 'ok', 'subscription', 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5)`); err == nil {
		t.Fatal("a subscription row was accepted before migration 58")
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	var costSource, createdAt string
	var cost, counted float64
	var tokens int
	if err := conn.QueryRow(`SELECT cost_source, cost_usd, counted_cost_usd, input_tokens, created_at FROM jev_trial_decisions WHERE arm_id = 'old'`).
		Scan(&costSource, &cost, &counted, &tokens, &createdAt); err != nil {
		t.Fatalf("the row of 57 is gone: %v", err)
	}
	if costSource != "reported" || cost != 0.001 || counted != 0.001 || tokens != 100 || createdAt != "2026-11-14T00:00:00.000Z" {
		t.Errorf("the migrated row changed: %s %v %v %d %s", costSource, cost, counted, tokens, createdAt)
	}
	insert := func(arm, source, costUSD string, counted float64) error {
		_, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, provider, requested_model, status, cost_source, cost_usd, counted_cost_usd,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability)
			VALUES (2, ?, 'trader-v3', 'codex_chatgpt', 'm', 'ok', ?, `+costUSD+`, ?, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5)`, arm, source, counted)
		return err
	}
	if err := insert("sub", "subscription", "NULL", 0); err != nil {
		t.Fatalf("a subscription row (no USD, counted 0): %v", err)
	}
	for name, err := range map[string]error{
		"a subscription row with a USD cost":     insert("s2", "subscription", "0.01", 0),
		"a subscription row counted in USD":      insert("s3", "subscription", "NULL", 0.01),
		"an unknown cost source":                 insert("s4", "guess", "NULL", 0),
		"a second row for the same decision/arm": insert("sub", "subscription", "NULL", 0),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	var indexes int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'jev_trial_decisions' AND name LIKE 'jev_trial_decisions_%_idx'`).Scan(&indexes); err != nil || indexes != 3 {
		t.Errorf("indexes = %d (err %v), want 3", indexes, err)
	}
	soundSchema := func(c *sql.DB) {
		t.Helper()
		var check string
		if err := c.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
			t.Errorf("integrity_check = %q (%v)", check, err)
		}
		var n int
		if err := c.QueryRow(`SELECT COUNT(*) FROM jev_trial_decisions`).Scan(&n); err != nil || n != 2 {
			t.Errorf("rows = %d (%v), want 2", n, err)
		}
	}
	soundSchema(conn)
	// A new connection parses the edited schema from the file: the schema is sound for everyone, not only this connection.
	var dbFile string
	if err := conn.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&dbFile); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	soundSchema(other)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM jev_trial_decisions`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows after the down step = %d (err %v), want 1: only the subscription row is dropped", n, err)
	}
	if err := insert("again", "subscription", "NULL", 0); err == nil {
		t.Error("a subscription row was accepted after the down migration")
	}
}

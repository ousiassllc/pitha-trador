package migrationtests

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000058 lets jev_trial_decisions.cost_source be 'subscription' (issue
// #1081: the ChatGPT subscription through the Codex CLI). SQLite cannot alter a
// CHECK, so the migration rebuilds the table the ordinary way (create the new
// table, copy the rows, drop the old one, rename; no writable_schema). The test
// pins: every row of 57 survives the up step unchanged (ids and the AUTOINCREMENT
// position included), a subscription row must have no USD cost and count 0, the
// indexes and the foreign key are rebuilt as they were, and one down step restores
// the old CHECK, drops only the subscription rows, and leaves the rest as it was.
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
			VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm'), (2, 1, '7203', 't', 'trader', 'h2', '{}', 'trader-v3', '{}', 1, 'm'),
			(3, 1, '7203', 't', 'trader', 'h3', '{}', 'trader-v3', '{}', 1, 'm')`,
		`INSERT INTO jev_trial_decisions (id, jev_decision_id, arm_id, prompt_version, provider, requested_model, status, cost_source, cost_usd, counted_cost_usd,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability, input_tokens, created_at)
			VALUES (1, 1, 'old', 'trader-v3', 'openrouter', 'm', 'ok', 'reported', 0.001, 0.001, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5, 100, '2026-11-14T00:00:00.000Z')`,
		`INSERT INTO jev_trial_decisions (id, jev_decision_id, arm_id, prompt_version, requested_model, status, failure_code, http_status, error_excerpt, cost_source, cost_usd, counted_cost_usd)
			VALUES (2, 3, 'failed', 'trader-v3', 'm', 'failed', 'http_429', 429, 'rate limited', 'unknown', NULL, 0.002)`,
		// A deleted row leaves the AUTOINCREMENT position at 3: the next id must not be reused.
		`INSERT INTO jev_trial_decisions (id, jev_decision_id, arm_id, prompt_version, requested_model, status, cost_source, counted_cost_usd,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability)
			VALUES (3, 2, 'gone', 'trader-v3', 'm', 'ok', 'unknown', 0, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5)`,
		`DELETE FROM jev_trial_decisions WHERE id = 3`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	insert := func(arm, source, costUSD string, counted float64) error {
		_, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, provider, requested_model, status, cost_source, cost_usd, counted_cost_usd,
			direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability)
			VALUES (2, ?, 'trader-v3', 'codex_chatgpt', 'm', 'ok', ?, `+costUSD+`, ?, 'LONG', 'TREND', 'good', 0.1, 0.1, 0.5)`, arm, source, counted)
		return err
	}
	if err := insert("sub", "subscription", "NULL", 0); err == nil {
		t.Fatal("a subscription row was accepted before migration 58")
	}
	rowsOf57 := dumpRows(t, conn)
	indexesOf57 := indexSQL(t, conn)
	if len(rowsOf57) != 2 || len(indexesOf57) != 2 {
		t.Fatalf("seed: rows = %d, indexes = %d, want 2 and 2", len(rowsOf57), len(indexesOf57))
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if got := dumpRows(t, conn); !equalStrings(got, rowsOf57) {
		t.Errorf("the rows of 57 changed by the up step:\n got %q\nwant %q", got, rowsOf57)
	}
	indexesOf58 := indexSQL(t, conn)
	if len(indexesOf58) != 3 {
		t.Errorf("indexes = %v, want the 2 of 000054 plus jev_trial_decisions_provider_idx", indexesOf58)
	}
	for name, ddl := range indexesOf57 {
		if indexesOf58[name] != ddl {
			t.Errorf("index %s = %q, want %q", name, indexesOf58[name], ddl)
		}
	}
	if _, ok := indexesOf58["jev_trial_decisions_provider_idx"]; !ok {
		t.Error("jev_trial_decisions_provider_idx is missing")
	}
	requireRebuiltTable(t, conn, true)
	if err := insert("sub", "subscription", "NULL", 0); err != nil {
		t.Fatalf("a subscription row (no USD, counted 0): %v", err)
	}
	var id int
	if err := conn.QueryRow(`SELECT id FROM jev_trial_decisions WHERE arm_id = 'sub'`).Scan(&id); err != nil || id != 4 {
		t.Errorf("the new row got id %d (err %v), want 4: the AUTOINCREMENT position of 57 was lost", id, err)
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
	if _, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, cost_source, failure_code)
		VALUES (999, 'orphan', 'trader-v3', 'm', 'failed', 'unknown', 'timeout')`); err == nil {
		t.Error("a row for a missing jev_decisions row was accepted: the foreign key was lost")
	}
	soundSchema := func(c *sql.DB) {
		t.Helper()
		var check string
		if err := c.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
			t.Errorf("integrity_check = %q (%v)", check, err)
		}
		var n int
		if err := c.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
			t.Errorf("foreign_key_check rows = %d (%v), want 0", n, err)
		}
		if err := c.QueryRow(`SELECT COUNT(*) FROM jev_trial_decisions`).Scan(&n); err != nil || n != 3 {
			t.Errorf("rows = %d (%v), want 3", n, err)
		}
	}
	soundSchema(conn)
	// A new connection parses the schema from the file: it is sound for everyone, not only this connection.
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
	if got := dumpRows(t, conn); !equalStrings(got, rowsOf57) {
		t.Errorf("after the down step the rows are not those of 57 (only the subscription row goes):\n got %q\nwant %q", got, rowsOf57)
	}
	indexesDown := indexSQL(t, conn)
	if len(indexesDown) != len(indexesOf57) {
		t.Errorf("indexes after the down step = %v, want %v", indexesDown, indexesOf57)
	}
	for name, ddl := range indexesOf57 {
		if indexesDown[name] != ddl {
			t.Errorf("index %s after the down step = %q, want %q", name, indexesDown[name], ddl)
		}
	}
	requireRebuiltTable(t, conn, false)
	if err := insert("again", "subscription", "NULL", 0); err == nil {
		t.Error("a subscription row was accepted after the down migration")
	}
	// The old CHECK only needs counted_cost_usd >= 0: a reported row is still fine, and the id goes on from 57's position.
	if err := insert("plain", "reported", "0.01", 0.01); err != nil {
		t.Errorf("a reported row after the down migration: %v", err)
	}
	if err := conn.QueryRow(`SELECT id FROM jev_trial_decisions WHERE arm_id = 'plain'`).Scan(&id); err != nil || id != 5 {
		t.Errorf("the row after the down step got id %d (err %v), want 5", id, err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, cost_source, failure_code)
		VALUES (999, 'orphan', 'trader-v3', 'm', 'failed', 'unknown', 'timeout')`); err == nil {
		t.Error("a row for a missing jev_decisions row was accepted after the down step: the foreign key was lost")
	}
}

// The migration files must not rewrite the stored schema (writable_schema) nor touch sqlite_master.
func TestMigration000058_DoesNotUseWritableSchema(t *testing.T) {
	for _, f := range []string{"up", "down"} {
		raw, err := db.MigrationsFS.ReadFile("migrations/000058_add_subscription_cost_source_to_jev_trial_decisions." + f + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(raw))
		for _, banned := range []string{"writable_schema", "sqlite_master", "sqlite_schema"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s migration uses %s", f, banned)
			}
		}
	}
}

// requireRebuiltTable checks the stored CHECK of jev_trial_decisions: 'subscription' present for 58, absent for the old one,
// and that the rebuild left no *_new table or trigger behind.
func requireRebuiltTable(t *testing.T, c *sql.DB, wantSubscription bool) {
	t.Helper()
	var ddl string
	if err := c.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'jev_trial_decisions'`).Scan(&ddl); err != nil {
		t.Fatalf("jev_trial_decisions: %v", err)
	}
	if got := strings.Contains(ddl, "subscription"); got != wantSubscription {
		t.Errorf("stored schema mentions subscription = %v, want %v:\n%s", got, wantSubscription, ddl)
	}
	var n int
	if err := c.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'jev_trial_decisions_new%' OR (type = 'trigger' AND tbl_name = 'jev_trial_decisions')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("leftover rebuild objects = %d (%v), want 0", n, err)
	}
}

// dumpRows returns every column of every row of jev_trial_decisions (ordered by id) as one string per row.
func dumpRows(t *testing.T, c *sql.DB) []string {
	t.Helper()
	rows, err := c.Query(`SELECT * FROM jev_trial_decisions ORDER BY id`)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(cols, ",")+"|"+fmt.Sprintf("%#v", vals))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// indexSQL returns the CREATE INDEX text of every explicit index on jev_trial_decisions, by name.
func indexSQL(t *testing.T, c *sql.DB) map[string]string {
	t.Helper()
	rows, err := c.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'jev_trial_decisions' AND sql IS NOT NULL`)
	if err != nil {
		t.Fatalf("indexes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		out[name] = ddl
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

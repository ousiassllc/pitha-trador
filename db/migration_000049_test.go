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

// Migration 000049 (issue #935) creates the four disclosure skip tables. The
// constraints that keep the record honest hold (a verdict is one of three, an
// unknown has a reason and only an unknown has one, a decision has one record,
// a kind is one of the two the rule covers), jev_decisions and
// calibration_outcomes are untouched, and one down step drops the four tables
// and nothing else.
func TestMigration000049_CreatesTheDisclosureSkipTablesWithTheirConstraints(t *testing.T) {
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
	tables := []string{"disclosure_items", "disclosure_fetches", "disclosure_skip_records", "disclosure_skip_matches"}
	exists := func(table string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}
	exec := func(stmt string, args ...any) error {
		_, err := conn.Exec(stmt, args...)
		return err
	}

	if err := m.Migrate(48); err != nil {
		t.Fatalf("migrate to 48: %v", err)
	}
	for _, table := range tables {
		if exists(table) {
			t.Fatalf("%s exists before migration 49", table)
		}
	}
	if err := m.Migrate(49); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, table := range tables {
		if !exists(table) {
			t.Fatalf("%s missing after migration 49", table)
		}
	}

	if err := exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (1, 1, '7203', '2026-10-15T01:00:00Z', 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, 'm')`); err != nil {
		t.Fatal(err)
	}
	record := func(decision int, verdict string, reason any) error {
		return exec(`INSERT INTO disclosure_skip_records (jev_decision_id, symbol, decided_at, verdict, unknown_reason, window_from, rule_version, evaluated_at)
			VALUES (?, '7203', 't', ?, ?, 't', 1, 't')`, decision, verdict, reason)
	}
	if err := record(1, "unknown", "no_fetch"); err != nil {
		t.Fatalf("a valid unknown record was refused: %v", err)
	}
	for name, err := range map[string]error{
		"a second record for one decision": record(1, "skip", nil),
		"an unknown verdict":               record(2, "maybe", nil),
		"unknown without a reason":         record(2, "unknown", nil),
		"a reason on a skip":               record(2, "skip", "no_fetch"),
		"an unknown reason code":           record(2, "unknown", "whatever"),
		"a record for a missing decision":  record(99, "no_skip", nil),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	match := func(kind string) error {
		return exec(`INSERT INTO disclosure_skip_matches (record_id, published_at, title, kind) VALUES (1, 't', 'x', ?)`, kind)
	}
	if err := match("earnings"); err != nil {
		t.Fatalf("a valid match was refused: %v", err)
	}
	if err := match("dividend"); err == nil {
		t.Error("a kind the rule does not cover was accepted")
	}
	if err := exec(`INSERT INTO disclosure_fetches (symbol, fetched_at, outcome) VALUES ('7203', 't', 'timeout')`); err == nil {
		t.Error("a fetch outcome other than ok / error was accepted")
	}
	if err := exec(`INSERT INTO disclosure_items (symbol, source_id, published_at, title, first_seen_at) VALUES ('7203', 'a', 't', 'x', 't')`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO disclosure_items (symbol, source_id, published_at, title, first_seen_at) VALUES ('7203', 'a', 't', 'y', 't')`); err == nil {
		t.Error("a second disclosure with the same (symbol, source_id) was accepted")
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	for _, table := range tables {
		if exists(table) {
			t.Fatalf("%s still exists after the down migration", table)
		}
	}
	for _, table := range []string{"jev_decisions", "calibration_outcomes", "jev_model_changes"} {
		if !exists(table) {
			t.Fatalf("the down migration dropped %s", table)
		}
	}
}

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

// Migration 000055 creates jev_trial_skips (issue #1062): one row per (live
// decision, arm) the trial did not ask, with a reason. A skip is unique per pair,
// needs a live decision and an arm, and one down step drops the table and nothing
// else. It migrates 54 -> 55 and steps one back.
func TestMigration000055_CreatesTheSkipTableAndRoundTrips(t *testing.T) {
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

	if err := m.Migrate(54); err != nil {
		t.Fatalf("migrate to 54: %v", err)
	}
	if exists("jev_trial_skips") {
		t.Fatal("jev_trial_skips exists before migration 55")
	}
	if err := m.Migrate(55); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists("jev_trial_skips") {
		t.Fatal("jev_trial_skips missing after migration 55")
	}
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		VALUES (1, 1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	insert := func(decisionID int, arm, reason string) error {
		_, err := conn.Exec(`INSERT INTO jev_trial_skips (jev_decision_id, arm_id, reason) VALUES (?, ?, ?)`, decisionID, arm, reason)
		return err
	}
	if err := insert(1, "a", "key_not_set"); err != nil {
		t.Fatalf("valid skip: %v", err)
	}
	for name, err := range map[string]error{
		"duplicate (decision, arm)": insert(1, "a", "min_interval"),
		"unknown live decision":     insert(99, "b", "key_not_set"),
		"empty arm":                 insert(1, "", "key_not_set"),
		"empty reason":              insert(1, "b", ""),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if exists("jev_trial_skips") {
		t.Fatal("jev_trial_skips still exists after the down migration")
	}
	if !exists("jev_trial_decisions") || !exists("jev_decisions") {
		t.Fatal("the down migration dropped another table")
	}
}

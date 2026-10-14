package sqlitedb_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000033 adds the triple-barrier label columns to
// calibration_outcomes (issue #756): pre-existing rows are marked
// label_version=1 (legacy close-only) with NULL barrier/net return/entry
// price so they stay distinguishable, barrier_hit only accepts
// tp/sl/timeout, and the down migration removes the columns.
func TestMigration000033_AddsTripleBarrierColumnsAndBackfillsLegacyVersion(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db"))
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

	if err := m.Migrate(32); err != nil {
		t.Fatalf("migrate to 32: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('7203', 'x', 'TSE', 1, 't', 't')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id, created_at)
		VALUES (1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm', 't')`); err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, was_direction_correct)
		VALUES (1, 5, 0.1, -0.1, 0.2, 1)`); err != nil {
		t.Fatalf("insert legacy outcome: %v", err)
	}

	if err := m.Migrate(33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}
	var (
		barrier         sql.NullString
		net, entryPrice sql.NullFloat64
		version         int
	)
	if err := conn.QueryRow(`SELECT barrier_hit, net_return_bps, entry_ref_price, label_version FROM calibration_outcomes`).
		Scan(&barrier, &net, &entryPrice, &version); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if barrier.Valid || net.Valid || entryPrice.Valid || version != 1 {
		t.Errorf("legacy row = %v/%v/%v/version %d, want NULL/NULL/NULL/version 1", barrier, net, entryPrice, version)
	}

	if _, err := conn.Exec(`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, barrier_hit, label_version)
		VALUES (1, 10, 0, 0, 0, 'bogus', 2)`); err == nil {
		t.Error("insert with barrier_hit='bogus' succeeded, want CHECK constraint failure")
	}

	if err := m.Migrate(32); err != nil {
		t.Fatalf("migrate down to 32: %v", err)
	}
	if _, err := conn.Exec(`SELECT label_version FROM calibration_outcomes`); err == nil {
		t.Error("label_version still present after down migration")
	}
}

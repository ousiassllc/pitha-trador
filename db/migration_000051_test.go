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

// Migration 000051 (issue #1053) adds disclosure_skip_records.unknown_detail:
// the finer reason of an unknown record, stored when it is finalized. Records
// that exist before it keep a NULL detail (no backfill), the column only takes
// the known detail codes and only on an unknown record, and one down step
// drops the column and nothing else.
func TestMigration000051_AddsTheUnknownDetailColumn(t *testing.T) {
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
	exec := func(stmt string, args ...any) error {
		_, err := conn.Exec(stmt, args...)
		return err
	}
	hasColumn := func() bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('disclosure_skip_records') WHERE name = 'unknown_detail'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	if err := m.Migrate(49); err != nil {
		t.Fatalf("migrate to 49: %v", err)
	}
	if hasColumn() {
		t.Fatal("unknown_detail exists before migration 51")
	}
	if err := exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 4; id++ {
		if err := exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id)
			VALUES (?, 1, '7203', '2026-10-15T01:00:00Z', 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, 'm')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec(`INSERT INTO disclosure_skip_records (jev_decision_id, symbol, decided_at, verdict, unknown_reason, window_from, rule_version, evaluated_at)
		VALUES (1, '7203', 't', 'unknown', 'no_fetch', 't', 1, 't')`); err != nil {
		t.Fatal(err)
	}

	if err := m.Migrate(51); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !hasColumn() {
		t.Fatal("unknown_detail missing after migration 51")
	}
	var old sql.NullString
	if err := conn.QueryRow(`SELECT unknown_detail FROM disclosure_skip_records WHERE jev_decision_id = 1`).Scan(&old); err != nil || old.Valid {
		t.Fatalf("an existing record's unknown_detail = %v, %v; want NULL (no backfill)", old, err)
	}

	record := func(decision int, verdict string, reason, detail any) error {
		return exec(`INSERT INTO disclosure_skip_records (jev_decision_id, symbol, decided_at, verdict, unknown_reason, unknown_detail, window_from, rule_version, evaluated_at)
			VALUES (?, '7203', 't', ?, ?, ?, 't', 1, 't')`, decision, verdict, reason, detail)
	}
	for _, detail := range []string{"fetch_failed", "lunch_break", "after_close", "within_fetch_lag", "dropped_from_candidates", "no_fetch_at_all", "undated_items", "item_limit"} {
		if err := exec(`UPDATE disclosure_skip_records SET unknown_detail = ? WHERE jev_decision_id = 1`, detail); err != nil {
			t.Errorf("detail %q was refused: %v", detail, err)
		}
	}
	for name, err := range map[string]error{
		"an unknown detail code": record(2, "unknown", "no_fetch", "whatever"),
		"a detail on a skip":     record(3, "skip", nil, "lunch_break"),
		"a detail on a no_skip":  record(4, "no_skip", nil, "item_limit"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := record(2, "skip", nil, nil); err != nil {
		t.Errorf("a record without a detail was refused: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	if hasColumn() {
		t.Fatal("unknown_detail still exists after the down migration")
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM disclosure_skip_records`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("records after the down migration = %d, %v; want the 2 rows kept", n, err)
	}
}

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

// Migration 000032 adds the order-flow feature columns to market_snapshots
// and jev_decisions (issue #750): rows that predate it read back as NULL
// ("not measured", never 0), and the down migration removes the columns.
func TestMigration000032_AddsOrderFlowColumnsAsNullable(t *testing.T) {
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

	if err := m.Migrate(31); err != nil {
		t.Fatalf("migrate to 31: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('7203', 'x', 'TSE', 1, 't', 't')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, vwap, price_vs_vwap_bps, volume, turnover, raw_data_json, created_at)
		VALUES (1, '7203', 't', 100, 100, 0, 0, 0, '{}', 't')`); err != nil {
		t.Fatalf("insert legacy snapshot: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id, created_at)
		VALUES (1, '7203', 't', 'scout', 'h', '{}', 'scout-v3', '{}', 1, 'm', 't')`); err != nil {
		t.Fatalf("insert legacy decision: %v", err)
	}

	if err := m.Migrate(32); err != nil {
		t.Fatalf("migrate to 32: %v", err)
	}
	for _, table := range []string{"market_snapshots", "jev_decisions"} {
		var ofi, window, dev sql.NullFloat64
		if err := conn.QueryRow(`SELECT ofi_short, ofi_window_long_seconds, microprice_dev_bps FROM `+table).Scan(&ofi, &window, &dev); err != nil { //nolint:gosec // G202: table is a constant from the list above
			t.Fatalf("%s: read legacy row: %v", table, err)
		}
		if ofi.Valid || window.Valid || dev.Valid {
			t.Errorf("%s: legacy row = %v/%v/%v, want NULLs", table, ofi, window, dev)
		}
	}

	if err := m.Migrate(31); err != nil {
		t.Fatalf("migrate down to 31: %v", err)
	}
	for _, table := range []string{"market_snapshots", "jev_decisions"} {
		if _, err := conn.Exec(`SELECT ofi_short FROM ` + table); err == nil { //nolint:gosec // G202: table is a constant from the list above
			t.Errorf("%s: ofi_short still present after down migration", table)
		}
	}
}

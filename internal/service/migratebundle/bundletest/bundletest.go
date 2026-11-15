// Package bundletest is the test support of the PC migration bundle (issue
// #1075): a temp SQLite database seeded with rows in several tables and with
// obviously fake dummy secrets, and a per-table row-count comparison. It is
// imported by tests only.
package bundletest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
)

// Dummy is the fake secret planted in the secrets table and in the
// machine-specific runtime settings; a bundle must not contain it anywhere.
const Dummy = "dummy-not-a-secret-xyz"

// OpenDB opens a fresh migrated database in a temp dir and returns it with
// its path.
func OpenDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pitha.db")
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, path
}

// OpenDBAtVersion returns a database migrated only up to schema version v
// (an older release's database), as a plain connection: no migration runs
// when it is opened or exported.
func OpenDBAtVersion(t *testing.T, v uint) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	conn, err := sql.Open("sqlite", sqlitedb.FileURI(path, "_foreign_keys=1&_busy_timeout=60000"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(v); err != nil {
		t.Fatalf("migrate to %d: %v", v, err)
	}
	return conn, path
}

// Exec runs the statements and fails the test on the first error.
func Exec(t *testing.T, conn *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := conn.ExecContext(context.Background(), s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

// Seed fills conn with rows in the main tables, the dummy secrets (secrets
// table, private key path, backup and log directory) and ordinary settings.
func Seed(t *testing.T, conn *sql.DB) {
	t.Helper()
	Exec(t, conn,
		`INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('7203', 'x', 'TSE', 1, 't', 't'), ('6758', 'y', 'TSE', 1, 't', 't')`,
		`INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed) VALUES (1, '7203', 't1', 'LONG', 'v1', 1), (2, '6758', 't2', 'SHORT', 'v1', 0)`,
		`INSERT INTO paper_orders (instrument_id, symbol, side, order_type, quantity, status, submitted_at) VALUES (1, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't')`,
		`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES
			(1, '7203', '2026-11-01T00:00:00Z', 1000, 10, 10000, 1000, 0, '{}'),
			(1, '7203', '2026-11-01T00:01:00Z', 1001, 10, 10010, 1000, 1, '{}'),
			(2, '6758', '2026-11-01T00:00:00Z', 2000, 5, 10000, 2000, 0, '{}')`,
		`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id) VALUES (1, '7203', 't', 'trader', 'h', '{}', 'q1', '{}', 5, 'm')`,
		`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion) VALUES (1, 5, 0.1, -0.1, 0.2), (1, 15, 0.2, -0.1, 0.3)`,
		`INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES ('t', 'daily_loss_limit', '{}')`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES
			('risk.daily_loss_limit_jpy', '50000', 't'),
			('screener.top_n', '7', 't'),
			('`+config.KeyBackupDir+`', '"/mnt/old-pc-`+Dummy+`/backups"', 't'),
			('`+config.KeyLogDir+`', '"/mnt/old-pc-`+Dummy+`/logs"', 't'),
			('`+config.KeyTachibanaDemoPrivateKeyPath+`', '"/home/old/`+Dummy+`/demo.pem"', 't')`,
		`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES
			('JEV_API_KEY', '`+Dummy+`-jev', 't'), ('KABU_API_PASSWORD', '`+Dummy+`-kabu', 't')`,
	)
}

// Counts returns the per-table row counts of conn, keyed by table name.
func Counts(t *testing.T, conn *sql.DB) map[string]int64 {
	t.Helper()
	tables, err := migratebundle.CountTables(context.Background(), conn)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	out := make(map[string]int64, len(tables))
	for _, tc := range tables {
		out[tc.Name] = tc.Rows
	}
	return out
}

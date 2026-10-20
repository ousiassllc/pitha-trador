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

// Migration 000041 (issue #824) adds instruments.asset_class and the bucket
// column of positions / paper_orders / trade_signals: rows that existed before
// it are filled with 'stock', only 'stock' and 'bond_etf' are accepted, and
// the down migration removes the columns again.
func TestMigration000041_BackfillsExistingRowsWithStock(t *testing.T) {
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

	if err := m.Migrate(40); err != nil {
		t.Fatalf("migrate to 40: %v", err)
	}
	mustExec(t, conn, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at)
		VALUES ('7203', 'x', 'TSE', 1, 't', 't')`)
	mustExec(t, conn, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed)
		VALUES (1, '7203', 't', 'LONG', 'v1', 1)`)
	mustExec(t, conn, `INSERT INTO paper_orders (instrument_id, symbol, side, order_type, quantity, status, submitted_at)
		VALUES (1, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't')`)
	mustExec(t, conn, `INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, opened_at)
		VALUES (1, 1, '7203', 'LONG', 100, 1000, 1000, 't')`)

	if err := m.Migrate(41); err != nil {
		t.Fatalf("migrate to 41: %v", err)
	}
	for _, q := range []string{
		`SELECT asset_class FROM instruments`,
		`SELECT bucket FROM trade_signals`,
		`SELECT bucket FROM paper_orders`,
		`SELECT bucket FROM positions`,
	} {
		var got string
		if err := conn.QueryRow(q).Scan(&got); err != nil || got != "stock" {
			t.Errorf("%s = %q (err %v), want stock", q, got, err)
		}
	}
	if _, err := conn.Exec(`UPDATE positions SET bucket = 'crypto'`); err == nil {
		t.Error("positions.bucket accepted 'crypto', want the CHECK to reject it")
	}
	if _, err := conn.Exec(`UPDATE instruments SET asset_class = 'crypto'`); err == nil {
		t.Error("instruments.asset_class accepted 'crypto', want the CHECK to reject it")
	}

	if err := m.Migrate(40); err != nil {
		t.Fatalf("migrate down to 40: %v", err)
	}
	if _, err := conn.Exec(`SELECT bucket FROM positions`); err == nil {
		t.Error("positions.bucket still exists after the down migration")
	}
}

func mustExec(t *testing.T, conn *sql.DB, query string) {
	t.Helper()
	if _, err := conn.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

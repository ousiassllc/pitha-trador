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

// Migration 000029 resets positions.unrealized_pnl to 0 on rows closed before
// issue #682 (the spec: closed rows carry 0, realized_pnl only), leaves open
// rows untouched, and its down migration is a no-op.
func TestMigration000029_ResetsClosedPositionsUnrealizedPnl(t *testing.T) {
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

	if err := m.Migrate(28); err != nil {
		t.Fatalf("migrate to 28: %v", err)
	}
	mustExec(t, conn, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES
		('7203', 'x', 'TSE', 1, 't', 't'), ('6758', 'y', 'TSE', 1, 't', 't')`)
	mustExec(t, conn, `INSERT INTO paper_orders (instrument_id, symbol, side, order_type, quantity, status, submitted_at)
		VALUES (1, '7203', 'BUY', 'MARKET', 100, 'FILLED', 't'), (2, '6758', 'BUY', 'MARKET', 100, 'FILLED', 't')`)
	// id 1: closed with a stale Mark value, id 2: open with a live value,
	// id 3: closed already at 0.
	mustExec(t, conn, `INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at) VALUES
		(1, 1, '7203', 'LONG', 100, 1000, 1085, 850, 800, 't', 't2'),
		(2, 2, '6758', 'LONG', 100, 1000, 1020, 200, NULL, 't', NULL),
		(1, 1, '7203', 'LONG', 100, 1000, 1000, 0, -50, 't', 't3')`)

	if err := m.Migrate(29); err != nil {
		t.Fatalf("migrate to 29: %v", err)
	}
	want := map[int]float64{1: 0, 2: 200, 3: 0}
	rows, err := conn.Query(`SELECT id, unrealized_pnl FROM positions`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var id int
		var pnl float64
		if err := rows.Scan(&id, &pnl); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if pnl != want[id] {
			t.Errorf("position %d unrealized_pnl = %v, want %v", id, pnl, want[id])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if seen != len(want) {
		t.Errorf("rows = %d, want %d", seen, len(want))
	}
	var realized float64
	if err := conn.QueryRow(`SELECT realized_pnl FROM positions WHERE id = 1`).Scan(&realized); err != nil || realized != 800 {
		t.Errorf("realized_pnl = %v (err %v), want 800 untouched", realized, err)
	}

	if err := m.Migrate(28); err != nil {
		t.Fatalf("migrate down to 28 (no-op): %v", err)
	}
}

func mustExec(t *testing.T, conn *sql.DB, q string) {
	t.Helper()
	if _, err := conn.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

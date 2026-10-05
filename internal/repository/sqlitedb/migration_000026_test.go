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

// Migration 000026 adds market_snapshots.special_quote/price_limit/lendable
// (issue #511): rows that predate it read back as "no restriction known"
// (0 / ” / NULL), and the down migration removes exactly those columns.
func TestMigration000026_AddsTradabilityColumnsWithSafeDefaults(t *testing.T) {
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

	if err := m.Migrate(25); err != nil {
		t.Fatalf("migrate to 25: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('7203', 'x', 'TSE', 1, 't', 't')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, vwap, price_vs_vwap_bps, volume, turnover, raw_data_json, created_at)
		VALUES (1, '7203', 't', 100, 100, 0, 0, 0, '{}', 't')`); err != nil {
		t.Fatalf("insert legacy snapshot: %v", err)
	}

	if err := m.Migrate(26); err != nil {
		t.Fatalf("migrate to 26: %v", err)
	}
	var special int
	var limit string
	var lendable sql.NullInt64
	if err := conn.QueryRow(`SELECT special_quote, price_limit, lendable FROM market_snapshots`).Scan(&special, &limit, &lendable); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if special != 0 || limit != "" || lendable.Valid {
		t.Errorf("legacy row = %d/%q/%v, want 0/''/NULL", special, limit, lendable)
	}
	if _, err := conn.Exec(`UPDATE market_snapshots SET price_limit = 'sideways'`); err == nil {
		t.Error("price_limit CHECK accepted an unknown value")
	}

	if err := m.Migrate(25); err != nil {
		t.Fatalf("migrate down to 25: %v", err)
	}
	if _, err := conn.Exec(`SELECT special_quote FROM market_snapshots`); err == nil {
		t.Error("special_quote still present after down migration")
	}
}

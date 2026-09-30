package judgement_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// newTestDB opens a fresh, fully migrated SQLite database in a temporary
// directory for a single test, closing it on cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return conn
}

// insertInstrument inserts a minimal instruments row directly (this package
// must not import the market package) and returns its ID, so the tests can
// satisfy the instrument_id foreign keys.
func insertInstrument(t *testing.T, db *sql.DB, symbol, name string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, ?, 'TSE Prime', 1)`, symbol, name)
	if err != nil {
		t.Fatalf("insert instrument fixture %q: %v", symbol, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument fixture id: %v", err)
	}
	return id
}

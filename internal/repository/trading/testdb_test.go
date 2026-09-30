package trading_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
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

// insertDecision inserts a minimal jev_decisions (decision_type=trader) row
// directly and returns its ID, for tests that need a real FK target for
// trade_signals.jev_decision_id.
func insertDecision(t *testing.T, db *sql.DB, instrumentID int64, symbol string, ts time.Time) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
		 VALUES (?, ?, ?, 'trader', 'hash', '{}', 'trader-v1', '{}', 0, 'test-model')`,
		instrumentID, symbol, sqlutil.FormatTime(ts))
	if err != nil {
		t.Fatalf("insert jev decision fixture: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("jev decision fixture id: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

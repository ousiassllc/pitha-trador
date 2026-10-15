package metalabel_test

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

// insertDecision inserts a minimal instruments + jev_decisions pair directly
// (this package must not import the judgement package) and returns the
// decision ID, so predictions can satisfy their jev_decision_id foreign key.
func insertDecision(t *testing.T, db *sql.DB, symbol string) int64 {
	t.Helper()
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, 'x', 'TSE Prime', 1)`, symbol)
	if err != nil {
		t.Fatalf("insert instrument fixture: %v", err)
	}
	instID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument fixture id: %v", err)
	}
	res, err = db.ExecContext(ctx, `INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id, created_at)
		VALUES (?, ?, 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm', 't')`, instID, symbol)
	if err != nil {
		t.Fatalf("insert decision fixture: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("decision fixture id: %v", err)
	}
	return id
}

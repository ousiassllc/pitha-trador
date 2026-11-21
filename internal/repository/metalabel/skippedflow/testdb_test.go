package skippedflow_test

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

// insertTrainingDecision stores a trader decision made at ts with direction
// and order-flow features, plus its stored state/response.
func insertTrainingDecision(t *testing.T, db *sql.DB, instID int64, symbol string, ts time.Time, direction, decisionType string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(), `INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id, created_at, ofi_short, ofi_mid, ofi_long, microprice_dev_bps)
		VALUES (?, ?, ?, ?, ?, '{"spread_bps":4}', 'trader-v3', '{"entry_quality":"good"}', ?, 0.7, 1, 'm', 't', 0.1, 0.2, 0.3, 1.5)`,
		instID, symbol, sqlutil.FormatTime(ts), decisionType, ts.String(), direction)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertOutcome(t *testing.T, db *sql.DB, decisionID int64, horizon, labelVersion int, netBps any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, net_return_bps, label_version)
		VALUES (?, ?, 0, 0, 0, ?, ?)`, decisionID, horizon, netBps, labelVersion); err != nil {
		t.Fatalf("insert outcome: %v", err)
	}
}

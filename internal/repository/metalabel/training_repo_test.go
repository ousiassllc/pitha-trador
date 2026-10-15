package metalabel_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

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

func TestRepository_ListTrainingRows(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)

	long := insertTrainingDecision(t, db, inst, "7203", base, "LONG", "trader")
	insertOutcome(t, db, long, 15, 2, 12.5)
	short := insertTrainingDecision(t, db, inst, "7203", base.Add(time.Hour), "SHORT", "trader")
	insertOutcome(t, db, short, 10, 2, -8.0)
	insertOutcome(t, db, short, 30, 2, 5.0) // newer label of the same decision: the one used

	// Rows that must not be returned.
	none := insertTrainingDecision(t, db, inst, "7203", base.Add(2*time.Hour), "NONE", "trader")
	insertOutcome(t, db, none, 15, 2, 1.0)
	legacy := insertTrainingDecision(t, db, inst, "7203", base.Add(3*time.Hour), "LONG", "trader")
	insertOutcome(t, db, legacy, 15, 1, nil)
	unlabeled := insertTrainingDecision(t, db, inst, "7203", base.Add(4*time.Hour), "LONG", "trader")
	_ = unlabeled
	scout := insertTrainingDecision(t, db, inst, "7203", base.Add(5*time.Hour), "LONG", "scout")
	insertOutcome(t, db, scout, 15, 2, 1.0)
	before := insertTrainingDecision(t, db, inst, "7203", base.Add(-time.Hour), "LONG", "trader")
	insertOutcome(t, db, before, 15, 2, 1.0)
	after := insertTrainingDecision(t, db, inst, "7203", base.Add(6*time.Hour), "LONG", "trader")
	insertOutcome(t, db, after, 15, 2, 1.0)

	got, err := r.ListTrainingRows(ctx, base, base.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("ListTrainingRows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %+v, want the LONG and the SHORT decision only", got)
	}
	if g := got[0]; g.JevDecisionID != long || g.Direction != "LONG" || g.Symbol != "7203" || !g.DecidedAt.Equal(base) ||
		g.HorizonMinutes != 15 || g.NetReturnBps != 12.5 || g.StateJSON != `{"spread_bps":4}` || g.ResponseJSON != `{"entry_quality":"good"}` {
		t.Errorf("first row = %+v", g)
	}
	if of := got[0].OrderFlow; of.OFIShort == nil || *of.OFIShort != 0.1 || *of.OFIMid != 0.2 || *of.OFILong != 0.3 || *of.MicropriceDevBps != 1.5 {
		t.Errorf("order flow = %+v", of)
	}
	if g := got[1]; g.JevDecisionID != short || g.Direction != "SHORT" || g.HorizonMinutes != 30 || g.NetReturnBps != 5.0 {
		t.Errorf("second row = %+v, want the newest label of the SHORT decision", g)
	}
	if end := got[0].LabelEnd(); !end.Equal(base.Add(15 * time.Minute)) {
		t.Errorf("LabelEnd = %v, want decision time + 15m", end)
	}
}

func TestRepository_ListTrainingRowsEmpty(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	got, err := r.ListTrainingRows(context.Background(), time.Unix(0, 0), time.Now())
	if err != nil || len(got) != 0 {
		t.Fatalf("ListTrainingRows = %v, %v, want none", got, err)
	}
}

package metalabel_test

import (
	"context"
	"database/sql"
	"errors"
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

// Issue #862: the count per label_version a skipped retraining reports. A
// decision counts once per version; the version 1 label has no net return, the
// version 2 label counts only with one (as ListTrainingRows reads it); decisions
// that are not LONG/SHORT trader decisions or are outside [from, to) do not.
// The second query is the one the issue asks to run on a real DB (there
// calibration_outcomes has no timestamp column, so the decision's is used).
func TestRepository_CountLabeledDecisionsPerLabelVersion(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	jst := time.FixedZone("JST", 9*3600)
	base := time.Date(2026, 10, 19, 9, 0, 0, 0, jst).UTC()

	// 10/19: three decisions labeled v1 and v2 (v1 has no net return), one v1 only.
	for i := range 3 {
		id := insertTrainingDecision(t, db, inst, "7203", base.Add(time.Duration(i)*time.Minute), "LONG", "trader")
		insertOutcome(t, db, id, 5, 1, nil) // (decision, horizon) is unique: the v1 label has its own horizon
		insertOutcome(t, db, id, 15, 2, 4.0)
	}
	v1Only := insertTrainingDecision(t, db, inst, "7203", base.Add(10*time.Minute), "SHORT", "trader")
	insertOutcome(t, db, v1Only, 15, 1, nil)
	// 10/20: two decisions with only a v2 label, one of them labeled twice (counts once).
	next := base.Add(24 * time.Hour)
	for i := range 2 {
		id := insertTrainingDecision(t, db, inst, "7203", next.Add(time.Duration(i)*time.Minute), "LONG", "trader")
		insertOutcome(t, db, id, 15, 2, 1.0)
		if i == 0 {
			insertOutcome(t, db, id, 30, 2, 2.0)
		}
	}
	// Not counted: a v2 label without a net return, a NONE decision, a scout decision, a decision before the range.
	noNet := insertTrainingDecision(t, db, inst, "7203", next.Add(20*time.Minute), "LONG", "trader")
	insertOutcome(t, db, noNet, 15, 2, nil)
	none := insertTrainingDecision(t, db, inst, "7203", next.Add(21*time.Minute), "NONE", "trader")
	insertOutcome(t, db, none, 15, 2, 1.0)
	scout := insertTrainingDecision(t, db, inst, "7203", next.Add(22*time.Minute), "LONG", "scout")
	insertOutcome(t, db, scout, 15, 2, 1.0)
	before := insertTrainingDecision(t, db, inst, "7203", base.Add(-time.Hour), "LONG", "trader")
	insertOutcome(t, db, before, 15, 2, 1.0)

	got, err := r.CountLabeledDecisions(ctx, base, next.Add(time.Hour))
	if err != nil {
		t.Fatalf("CountLabeledDecisions: %v", err)
	}
	if got.V1 != 4 || got.V2 != 5 {
		t.Errorf("counts = %+v, want V1 4 (3 + the v1-only one), V2 5 (3 + 2)", got)
	}
	if empty, err := r.CountLabeledDecisions(ctx, time.Unix(0, 0), time.Unix(1, 0)); err != nil || empty != (metalabel.LabelVersionCounts{}) {
		t.Errorf("empty range = %+v, %v, want zero counts", empty, err)
	}

	rows, err := db.QueryContext(ctx, `SELECT date(d.timestamp,'+9 hours') d, c.label_version, count(*)
		FROM calibration_outcomes c JOIN jev_decisions d ON d.id = c.jev_decision_id GROUP BY 1,2 ORDER BY 1,2`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day string
		var version, n int
		if err := rows.Scan(&day, &version, &n); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s label_version=%d rows=%d", day, version, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// Issue #881: the training reads the live prompt's cohort only. Two labeled
// decisions of trader-v3 and one of trader-v4 sit in the same window; which of
// them the model learns from, and how a skipped retraining counts them,
// follows the scope; an unreadable scope is an error, never a mixed set.
func TestRepository_TrainingLeavesOutForeignPromptVersions(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	ids := map[string]int64{}
	for i, version := range []string{"trader-v3", "trader-v3", "trader-v4"} {
		id := insertTrainingDecision(t, db, inst, "7203", base.Add(time.Duration(i)*time.Hour), "LONG", "trader")
		if _, err := db.ExecContext(ctx, `UPDATE jev_decisions SET question_version = ? WHERE id = ?`, version, id); err != nil {
			t.Fatal(err)
		}
		insertOutcome(t, db, id, 15, 2, 1.0)
		ids[version] = id
	}
	from, to := base, base.Add(10*time.Hour)
	training := func() []int64 {
		t.Helper()
		rows, err := r.ListTrainingRows(ctx, from, to)
		if err != nil {
			t.Fatalf("ListTrainingRows: %v", err)
		}
		var got []int64
		for _, row := range rows {
			got = append(got, row.JevDecisionID)
		}
		return got
	}

	if got := training(); len(got) != 3 {
		t.Fatalf("without a scope the training reads %v, want all three (the behaviour before #881)", got)
	}
	if c, err := r.CountLabeledDecisions(ctx, from, to); err != nil || c.V2 != 3 {
		t.Fatalf("CountLabeledDecisions without a scope = %+v, %v, want V2 3", c, err)
	}

	r.SetForeignTraderVersions(func(context.Context) ([]string, error) { return []string{"trader-v4"}, nil })
	if got := training(); len(got) != 2 || got[0] == ids["trader-v4"] || got[1] == ids["trader-v4"] {
		t.Fatalf("live trader-v3: the training reads %v, want the two trader-v3 decisions", got)
	}
	if c, err := r.CountLabeledDecisions(ctx, from, to); err != nil || c.V2 != 2 {
		t.Fatalf("live trader-v3: CountLabeledDecisions = %+v, %v, want V2 2", c, err)
	}

	// After a promotion the model starts from the trader-v4 decisions alone.
	r.SetForeignTraderVersions(func(context.Context) ([]string, error) {
		return []string{"trader-v1", "trader-v2", "trader-v3"}, nil
	})
	if got := training(); len(got) != 1 || got[0] != ids["trader-v4"] {
		t.Fatalf("live trader-v4: the training reads %v, want the trader-v4 decision %d only", got, ids["trader-v4"])
	}
	if c, err := r.CountLabeledDecisions(ctx, from, to); err != nil || c.V2 != 1 {
		t.Fatalf("live trader-v4: CountLabeledDecisions = %+v, %v, want V2 1", c, err)
	}

	r.SetForeignTraderVersions(func(context.Context) ([]string, error) { return nil, errors.New("settings unreadable") })
	if got, err := r.ListTrainingRows(ctx, from, to); err == nil {
		t.Fatalf("ListTrainingRows = %v, nil with an unreadable scope, want an error", got)
	}
	if _, err := r.CountLabeledDecisions(ctx, from, to); err == nil {
		t.Fatal("CountLabeledDecisions must fail with an unreadable scope")
	}
}

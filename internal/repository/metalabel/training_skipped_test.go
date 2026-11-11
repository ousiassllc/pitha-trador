package metalabel_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func insertCounterfactual(t *testing.T, db *sql.DB, decisionID int64, horizon int, net float64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, reject_reason, barrier_hit, net_return_bps, entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff)
		VALUES (?, ?, 'entry_quality_below_threshold', 'timeout', ?, 1000, 2, 0, 0, 0, '2026-09-20')`, decisionID, horizon, net); err != nil {
		t.Fatalf("insert counterfactual label: %v", err)
	}
}

func insertSignal(t *testing.T, db *sql.DB, instID, decisionID int64, ts time.Time, riskPassed bool, reason string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
		VALUES (?, ?, '7203', ?, 'NONE', 'policy-v1', ?, NULLIF(?, ''))`, instID, decisionID, ts.Format(time.RFC3339Nano), riskPassed, reason); err != nil {
		t.Fatalf("insert signal: %v", err)
	}
}

type skippedFixture struct {
	r          *metalabel.Repository
	db         *sql.DB
	base       time.Time
	to         time.Time
	entered    int64 // labeled in calibration_outcomes, its signal passed
	skipped    int64 // labeled in calibration_outcomes, skipped by a guard
	counter    int64 // no calibration_outcomes row; counterfactual label only
	none       int64 // Jev NONE; even a stray counterfactual row must not train
	doubleBoth int64 // a calibration_outcomes label and a counterfactual label
}

func newSkippedFixture(t *testing.T) skippedFixture {
	t.Helper()
	db := newTestDB(t)
	res, err := db.ExecContext(context.Background(), `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	f := skippedFixture{r: metalabel.NewRepository(db), db: db, base: base, to: base.Add(24 * time.Hour)}

	f.entered = insertTrainingDecision(t, db, inst, "7203", base, "LONG", "trader")
	insertOutcome(t, db, f.entered, 15, 2, 12.5)
	insertSignal(t, db, inst, f.entered, base, true, "")
	f.skipped = insertTrainingDecision(t, db, inst, "7203", base.Add(time.Hour), "SHORT", "trader")
	insertOutcome(t, db, f.skipped, 15, 2, -8)
	insertSignal(t, db, inst, f.skipped, base.Add(time.Hour), false, "entry_quality_below_threshold")
	f.counter = insertTrainingDecision(t, db, inst, "7203", base.Add(2*time.Hour), "LONG", "trader")
	insertSignal(t, db, inst, f.counter, base.Add(2*time.Hour), false, "not_calibrated")
	insertCounterfactual(t, db, f.counter, 15, 4.5)
	f.none = insertTrainingDecision(t, db, inst, "7203", base.Add(3*time.Hour), "NONE", "trader")
	insertCounterfactual(t, db, f.none, 15, 9)
	f.doubleBoth = insertTrainingDecision(t, db, inst, "7203", base.Add(4*time.Hour), "LONG", "trader")
	insertOutcome(t, db, f.doubleBoth, 15, 2, 3)
	insertCounterfactual(t, db, f.doubleBoth, 15, 99)
	return f
}

func ids(rows []metalabel.TrainingRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.JevDecisionID)
	}
	return out
}

// With the switch off (the default) the training rows are exactly the
// calibration_outcomes rows they were before issue #1052: entered and skipped
// decisions alike, and nothing from counterfactual_labels.
func TestListTrainingRows_DefaultRowSetIsTheCalibrationOutcomesRows(t *testing.T) {
	f := newSkippedFixture(t)
	got, err := f.r.ListTrainingRows(context.Background(), f.base, f.to)
	if err != nil {
		t.Fatalf("ListTrainingRows: %v", err)
	}
	if want := []int64{f.entered, f.skipped, f.doubleBoth}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("default rows = %v, want %v (calibration_outcomes rows only)", ids(got), want)
	}
	for _, row := range got {
		if row.SkippedLabel {
			t.Errorf("decision %d is a counterfactual row under the default scope", row.JevDecisionID)
		}
	}
	if row := got[2]; row.NetReturnBps != 3 {
		t.Errorf("a decision with both labels uses the calibration_outcomes number: net = %v, want 3", row.NetReturnBps)
	}
}

// With the switch on, only the skipped candidates the live labeling never
// labeled are added; a NONE decision is never a row and a decision with a
// calibration_outcomes label is not a row twice.
func TestListTrainingRowsWithSkipped_AddsOnlyUnlabeledSkippedLongShort(t *testing.T) {
	f := newSkippedFixture(t)
	got, err := f.r.ListTrainingRowsWithSkipped(context.Background(), f.base, f.to)
	if err != nil {
		t.Fatalf("ListTrainingRowsWithSkipped: %v", err)
	}
	if want := []int64{f.entered, f.skipped, f.counter, f.doubleBoth}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("rows with skipped labels = %v, want %v in decision order", ids(got), want)
	}
	for _, row := range got {
		if row.JevDecisionID == f.counter {
			if !row.SkippedLabel || row.NetReturnBps != 4.5 || row.HorizonMinutes != 15 || row.Direction != "LONG" {
				t.Errorf("counterfactual row = %+v, want skipped LONG 15m net 4.5", row)
			}
		} else if row.SkippedLabel {
			t.Errorf("decision %d is marked as a counterfactual row", row.JevDecisionID)
		}
	}

	// The rows of the default scope are all still there with the same numbers.
	base, err := f.r.ListTrainingRows(context.Background(), f.base, f.to)
	if err != nil {
		t.Fatal(err)
	}
	var kept []metalabel.TrainingRow
	for _, row := range got {
		if !row.SkippedLabel {
			kept = append(kept, row)
		}
	}
	if !reflect.DeepEqual(kept, base) {
		t.Fatalf("the default rows changed when the skipped labels were added:\n got %+v\nwant %+v", kept, base)
	}
}

// The calibration_outcome_origins view tells an entered decision's label from a
// skipped one and a no-direction one, without changing calibration_outcomes.
func TestCalibrationOutcomeOrigins_MarksEnteredSkippedAndNoDirection(t *testing.T) {
	f := newSkippedFixture(t)
	var inst int64
	if err := f.db.QueryRow(`SELECT id FROM instruments LIMIT 1`).Scan(&inst); err != nil {
		t.Fatal(err)
	}
	noneLabeled := insertTrainingDecision(t, f.db, inst, "7203", f.base.Add(5*time.Hour), "NONE", "trader")
	insertOutcome(t, f.db, noneLabeled, 15, 1, nil)
	insertSignal(t, f.db, inst, noneLabeled, f.base.Add(5*time.Hour), false, "jev_none")

	var before int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM calibration_outcomes`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	want := map[int64][2]string{
		f.entered:    {"entered", ""},
		f.skipped:    {"skipped", "entry_quality_below_threshold"},
		f.doubleBoth: {"skipped", ""},
		noneLabeled:  {"no_direction", "jev_none"},
	}
	rows, err := f.db.Query(`SELECT jev_decision_id, origin, COALESCE(reject_reason, '') FROM calibration_outcome_origins`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[int64][2]string{}
	for rows.Next() {
		var id int64
		var origin, reason string
		if err := rows.Scan(&id, &origin, &reason); err != nil {
			t.Fatal(err)
		}
		got[id] = [2]string{origin, reason}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	if len(got) != before {
		t.Fatalf("the view has %d rows, calibration_outcomes %d: it must be one row per outcome", len(got), before)
	}
}

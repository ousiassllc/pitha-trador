package fromsplit_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence/fromsplit"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// TestIndependenceUsesTheRealWalkForwardWindows splits synthetic rows with the
// training package, and checks that every row the model used or was tested on
// is not independent while the first decision after LabelEnd + embargo is.
func TestIndependenceUsesTheRealWalkForwardWindows(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	start := time.Date(2026, 10, 19, 9, 0, 0, 0, jst)
	var rows []training.Row
	for i := range 80 {
		d := start.Add(time.Duration(i) * 30 * time.Minute)
		rows = append(rows, training.Row{JevDecisionID: int64(i + 1), DecidedAt: d, LabelEnd: d.Add(10 * time.Minute)})
	}
	cfg := training.WalkForwardConfig{
		OOSMinRows: 10, OOSMinDays: 1, OOSMaxWindow: 21 * 24 * time.Hour, CalibrationFraction: 0.25,
		Embargo: 30 * time.Minute, MinFit: 10, MinCalibration: 5,
	}
	now := start.Add(80 * 30 * time.Minute)
	split, err := training.SplitWalkForward(rows, now, start.Add(-time.Hour), cfg)
	if err != nil {
		t.Fatalf("SplitWalkForward: %v", err)
	}
	w := fromsplit.Windows(split, cfg)
	w.EvalFrom, w.EvalTo = start, now.Add(48*time.Hour)
	ic := independence.Config{Windows: w}

	used := 0
	for _, set := range [][]training.Row{split.Fit, split.Calibrate} {
		for _, r := range set {
			used++
			if got := ic.Classify(independence.Row{ID: r.JevDecisionID, DecidedAt: r.DecidedAt}); got.Class != independence.Excluded {
				t.Errorf("fit/calibration row %d classified %+v", r.JevDecisionID, got)
			}
		}
	}
	if used == 0 || len(split.OOS) == 0 {
		t.Fatalf("degenerate split: %d used, %d OOS", used, len(split.OOS))
	}
	for _, r := range split.OOS {
		if got := ic.Classify(independence.Row{ID: r.JevDecisionID, DecidedAt: r.DecidedAt}); got.Class != independence.Reference {
			t.Errorf("OOS row %d classified %+v, want reference", r.JevDecisionID, got)
		}
	}
	var lastLabelEnd time.Time
	for _, r := range rows {
		if r.LabelEnd.After(lastLabelEnd) && !r.LabelEnd.After(now) {
			lastLabelEnd = r.LabelEnd
		}
	}
	boundary := lastLabelEnd.Add(cfg.Embargo)
	if got := ic.Classify(independence.Row{DecidedAt: boundary}); got.Reason != independence.ReasonLabelHorizon && got.Reason != independence.ReasonNotAfterOOSEnd {
		t.Errorf("at LabelEnd + embargo: %+v", got)
	}
	if got := ic.Classify(independence.Row{DecidedAt: boundary.Add(time.Nanosecond)}); got.Class != independence.Independent {
		t.Errorf("just after LabelEnd + embargo: %+v", got)
	}
}

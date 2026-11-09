package independence_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

var base = time.Date(2026, 10, 19, 9, 0, 0, 0, time.FixedZone("JST", 9*3600))

func at(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

// windows: train [0, 100], calibration end 90, original OOS [200, 300),
// LabelEnd 310 and a 30 minute embargo (independent after 340), cutoff before
// everything, evaluation window [0, 1000).
func windows() manifest.Windows {
	return manifest.Windows{
		TrainStart: at(0), TrainEnd: at(100), CalibrationEnd: at(90), OOSStart: at(200), OOSEnd: at(300),
		LabelEnd: at(310), EmbargoSeconds: 1800, LLMCutoff: at(-10), EvalFrom: at(0), EvalTo: at(1000),
	}
}

func classify(w manifest.Windows, t time.Time) independence.Result {
	return independence.Config{Windows: w}.Classify(independence.Row{ID: 1, DecidedAt: t})
}

func TestBoundaryValues(t *testing.T) {
	ns := time.Nanosecond
	for _, tc := range []struct {
		name string
		t    time.Time
		want independence.Result
	}{
		{"before train_start", base.Add(-ns), independence.Result{independence.Excluded, independence.ReasonBeforeTrainStart}},
		{"at train_start", at(0), independence.Result{independence.Excluded, independence.ReasonTrainingWindow}},
		{"at train end", at(100), independence.Result{independence.Excluded, independence.ReasonTrainingWindow}},
		{"just after train end", at(100).Add(ns), independence.Result{independence.Excluded, independence.ReasonGapBetweenWindows}},
		{"just before oos start", at(200).Add(-ns), independence.Result{independence.Excluded, independence.ReasonGapBetweenWindows}},
		{"at oos start", at(200), independence.Result{independence.Reference, independence.ReasonOriginalOOS}},
		{"just before oos end", at(300).Add(-ns), independence.Result{independence.Reference, independence.ReasonOriginalOOS}},
		{"at oos end", at(300), independence.Result{independence.Excluded, independence.ReasonNotAfterOOSEnd}},
		{"just after oos end", at(300).Add(ns), independence.Result{independence.Excluded, independence.ReasonLabelHorizon}},
		{"at LabelEnd", at(310), independence.Result{independence.Excluded, independence.ReasonLabelHorizon}},
		{"embargo: at LabelEnd + embargo", at(340), independence.Result{independence.Excluded, independence.ReasonLabelHorizon}},
		{"embargo: just after LabelEnd + embargo", at(340).Add(ns), independence.Result{independence.Independent, independence.ReasonIndependent}},
		{"at eval_to", at(1000), independence.Result{independence.Excluded, independence.ReasonOutsideEvalWindow}},
		{"just before eval_to", at(1000).Add(-ns), independence.Result{independence.Independent, independence.ReasonIndependent}},
	} {
		if got := classify(windows(), tc.t); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCalibrationEndCountsWhenItIsTheLatestEnd(t *testing.T) {
	w := windows()
	w.CalibrationEnd = at(120) // later than TrainEnd
	if got := classify(w, at(120)); got.Reason != independence.ReasonTrainingWindow {
		t.Errorf("at calibration end: %+v", got)
	}
	if got := classify(w, at(120).Add(time.Nanosecond)); got.Reason != independence.ReasonGapBetweenWindows {
		t.Errorf("after calibration end: %+v", got)
	}
}

func TestEmbargoDefinesTheHorizon(t *testing.T) {
	w := windows()
	w.EmbargoSeconds = 0
	if got := classify(w, at(310)); got.Reason != independence.ReasonLabelHorizon {
		t.Errorf("at LabelEnd without embargo: %+v", got)
	}
	if got := classify(w, at(310).Add(time.Nanosecond)); got.Class != independence.Independent {
		t.Errorf("after LabelEnd without embargo: %+v", got)
	}
	w.EmbargoSeconds = 3600
	if got := classify(w, at(370)); got.Reason != independence.ReasonLabelHorizon {
		t.Errorf("at LabelEnd + 60m embargo: %+v", got)
	}
}

func TestLLMCutoffAloneDoesNotDecide(t *testing.T) {
	w := windows()
	// A row after the cutoff that the model trained on is not independent.
	if got := classify(w, at(50)); got.Class != independence.Excluded || got.Reason != independence.ReasonTrainingWindow {
		t.Errorf("after cutoff but in the training window: %+v", got)
	}
	// A row after the cutoff in the original OOS is reference, not independent.
	if got := classify(w, at(250)); got.Class != independence.Reference {
		t.Errorf("after cutoff in the original OOS: %+v", got)
	}
	// With the cutoff after the windows, a row after the boundary but before
	// the cutoff is excluded and the cutoff instant itself is independent.
	w.LLMCutoff = at(400)
	if got := classify(w, at(400).Add(-time.Nanosecond)); got.Reason != independence.ReasonBeforeLLMCutoff {
		t.Errorf("just before the cutoff: %+v", got)
	}
	if got := classify(w, at(400)); got.Class != independence.Independent {
		t.Errorf("at the cutoff: %+v", got)
	}
	// An early cutoff does not make a gap row independent.
	if got := classify(windows(), at(150)); got.Class != independence.Excluded {
		t.Errorf("gap row after cutoff: %+v", got)
	}
}

func TestEvaluationWindowStart(t *testing.T) {
	w := windows()
	w.EvalFrom = at(500)
	if got := classify(w, at(500).Add(-time.Nanosecond)); got.Reason != independence.ReasonOutsideEvalWindow {
		t.Errorf("before eval_from: %+v", got)
	}
	if got := classify(w, at(500)); got.Class != independence.Independent {
		t.Errorf("at eval_from: %+v", got)
	}
}

func TestCohortMismatchIsExcluded(t *testing.T) {
	cfg := independence.Config{Windows: windows(), Cohort: "jev-v2"}
	row := independence.Row{DecidedAt: at(500), Cohort: "jev-v1"}
	if got := cfg.Classify(row); got != (independence.Result{Class: independence.Excluded, Reason: independence.ReasonCohortMismatch}) {
		t.Errorf("another cohort: %+v", got)
	}
	row.Cohort = ""
	if got := cfg.Classify(row); got.Reason != independence.ReasonCohortMismatch {
		t.Errorf("no cohort: %+v", got)
	}
	row.Cohort = "jev-v2"
	if got := cfg.Classify(row); got.Class != independence.Independent {
		t.Errorf("same cohort: %+v", got)
	}
	// A cohort-mismatched row in the original OOS is excluded too, not reference.
	row = independence.Row{DecidedAt: at(250), Cohort: "jev-v1"}
	if got := cfg.Classify(row); got.Class != independence.Excluded {
		t.Errorf("cohort mismatch in the OOS: %+v", got)
	}
}

func TestSplitCountsEveryClass(t *testing.T) {
	cfg := independence.Config{Windows: windows()}
	rows := []independence.Row{
		{ID: 1, DecidedAt: at(-5)}, {ID: 2, DecidedAt: at(50)}, {ID: 3, DecidedAt: at(150)},
		{ID: 4, DecidedAt: at(250)}, {ID: 5, DecidedAt: at(320)}, {ID: 6, DecidedAt: at(400)}, {ID: 7, DecidedAt: at(500)},
	}
	ind, ref, s := cfg.Split(rows)
	if len(ind) != 2 || ind[0] != 6 || ind[1] != 7 || len(ref) != 1 || ref[0] != 4 {
		t.Fatalf("independent %v reference %v", ind, ref)
	}
	if s.Independent != 2 || s.Reference != 1 || s.Excluded != 4 {
		t.Errorf("summary %+v", s)
	}
	for reason, n := range map[string]int{
		independence.ReasonBeforeTrainStart: 1, independence.ReasonTrainingWindow: 1, independence.ReasonGapBetweenWindows: 1,
		independence.ReasonLabelHorizon: 1, independence.ReasonOriginalOOS: 1, independence.ReasonIndependent: 2,
	} {
		if s.ByReason[reason] != n {
			t.Errorf("reason %s = %d, want %d", reason, s.ByReason[reason], n)
		}
	}
	if cfg.Describe() == "" {
		t.Error("empty description")
	}
}

func TestZeroValueIsNotIndependent(t *testing.T) {
	var r independence.Result
	if r.Class == independence.Independent {
		t.Fatal("the zero Result is independent")
	}
}

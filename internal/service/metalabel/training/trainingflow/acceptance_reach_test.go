package trainingflow_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// rowsPerDay is the usable labeled decisions a trading day produced on
// 2026-10-19/20/21 (145, 114, 99): the synthetic history below has about that.
const rowsPerDay = 120

// legacyBar and legacyWalk are the conditions before issue #862: an OOS of the
// last 7 calendar days, whatever the bar asks for (the "reach back" is made
// unreachable), judged on at least 1084 rows, 542 takes and 5 trading days.
func legacyBar() training.AcceptanceConfig {
	c := training.DefaultAcceptanceConfig()
	c.MinOOSSamples, c.MinTakes, c.MinOOSDays = 1084, 542, 5
	return c
}

func legacyWalk() training.WalkForwardConfig {
	c := training.DefaultWalkForwardConfig()
	c.OOSMinRows, c.OOSMinDays, c.OOSMaxWindow = math.MaxInt32, math.MaxInt32, 7*24*time.Hour
	return c
}

// judged reports whether a stored model got as far as the confidence-interval
// criteria (or was adopted): the OOS window held the samples and days the bar
// asks for. A model rejected for too few samples/days was not judged at all.
func judged(m repo.Model) bool {
	return m.Status == repo.ModelActive || (m.RejectReason != training.RejectOOSSamples && m.RejectReason != training.RejectOOSDays)
}

// Issue #862: at the current data volume (about 120 usable rows a trading day)
// the proposed conditions reach an adoption decision after a finite number of
// trading days; the conditions before (fixed 7-day OOS, 1084 rows) never do,
// however long the history grows.
func TestAcceptance_ReachableAtCurrentDataVolume(t *testing.T) {
	ctx := context.Background()
	history := func(days int) *fakeSource {
		return &fakeSource{rows: noiseRows(days, rowsPerDay, 0.02, 0.25, edgeSeed)}
	}

	first := 0
	for days := 1; days <= 20 && first == 0; days++ {
		m, err := defaultBarTrainer(history(days), &memStore{}).Train(ctx)
		switch {
		case errors.Is(err, training.ErrInsufficientData):
			continue
		case err != nil:
			t.Fatalf("%d days: Train: %v", days, err)
		case judged(m):
			first = days
			t.Logf("proposed conditions: the first adoption decision after %d trading days of %d rows (status %s %s, OOS %d rows on %d days)",
				days, rowsPerDay, m.Status, m.RejectReason, m.OOSSamples, m.OOSDays)
			if m.Status != repo.ModelActive {
				t.Errorf("%d days: status %s (%s), want a model with a real edge adopted", days, m.Status, m.RejectReason)
			}
		}
	}
	if first == 0 {
		t.Fatal("the proposed conditions did not reach an adoption decision within 20 trading days")
	}
	// 7 trading days of OOS + more than the 130 rows fit and calibration need:
	// 8 days leave 120 rows, 9 days leave 240.
	if first != 9 {
		t.Errorf("first decision after %d trading days, want 9 (7 OOS days + 2 days of fit and calibration)", first)
	}

	// The same history, one day short, is skipped with the shortfall.
	_, err := defaultBarTrainer(history(first-1), &memStore{}).Train(ctx)
	var skip *training.InsufficientDataError
	if !errors.As(err, &skip) || skip.Code != repo.SkipTooFewRowsBeforeOOS || skip.Usable != rowsPerDay || skip.Required != 130 {
		t.Errorf("%d days: error = %v, want too_few_rows_before_oos, %d of 130", first-1, err, rowsPerDay)
	}

	// Before: the 7-day OOS holds about 5 trading days, 600 rows, and 1084 are
	// needed. A month or two of history changes nothing.
	for _, days := range []int{10, 20, 30, 60} {
		m, err := defaultBarTrainer(history(days), &memStore{},
			training.WithAcceptance(legacyBar()), training.WithWalkForward(legacyWalk())).Train(ctx)
		if err != nil {
			t.Fatalf("legacy, %d days: Train: %v", days, err)
		}
		if judged(m) || m.RejectReason != training.RejectOOSSamples || m.OOSSamples >= 1084 {
			t.Errorf("legacy, %d days: status %s (%s) on %d OOS rows, want oos_samples_below_minimum below 1084", days, m.Status, m.RejectReason, m.OOSSamples)
		}
	}
}

// Issue #862: the guards are intact. A model whose OOS EV lower limit is not
// above 0, whose AUC lower limit is not above 0.5, or that does not beat the
// model in force on the same OOS rows is not adopted, with the reason recorded;
// the proposed bar does not lower any of them.
func TestAcceptance_RejectsModelThatDoesNotBeatBaseline(t *testing.T) {
	ctx := context.Background()

	// No edge at all (outcomes independent of every feature, strong day effect):
	// judged on a full OOS window, never adopted; at least one seed gets as far
	// as the interval criteria (the rest take too few of the OOS rows).
	byInterval := 0
	for seed := range uint64(5) {
		store := &memStore{}
		m, err := defaultBarTrainer(&fakeSource{rows: noiseRows(12, rowsPerDay, 0.12, 0, 4000+seed)}, store).Train(ctx)
		if err != nil {
			t.Fatalf("seed %d: Train: %v", seed, err)
		}
		switch {
		case m.Status == repo.ModelRejected && (m.RejectReason == training.RejectEV || m.RejectReason == training.RejectAUC):
			byInterval++
		case m.Status == repo.ModelRejected && m.RejectReason == training.RejectTakes:
		default:
			t.Errorf("seed %d: status %s (%s), want rejected by the EV/AUC lower limit or too few takes", seed, m.Status, m.RejectReason)
		}
		if m.OOSSamples < training.DefaultAcceptanceConfig().MinOOSSamples || m.OOSDays < training.DefaultAcceptanceConfig().MinOOSDays {
			t.Errorf("seed %d: OOS %d rows on %d days, want the window the bar asks for", seed, m.OOSSamples, m.OOSDays)
		}
		if _, found, _ := store.LatestActiveModel(ctx); found {
			t.Errorf("seed %d: a model without an edge is in force", seed)
		}
	}
	if byInterval == 0 {
		t.Error("no seed was rejected by the EV or AUC lower limit")
	}

	// The lower limits stay at EV > 0 and AUC > 0.5 and the criteria that judge
	// them reject the boundary itself.
	bar := training.DefaultAcceptanceConfig()
	if bar.MinEVLowerBps != 0 || bar.MinAUCLower != 0.5 {
		t.Errorf("bar lower limits = EV %v, AUC %v, want 0 and 0.5", bar.MinEVLowerBps, bar.MinAUCLower)
	}
	f := func(v float64) *float64 { return &v }
	e := training.Evaluation{Samples: bar.MinOOSSamples, Takes: bar.MinTakes, AUC: f(0.6), EVBps: f(8)}
	for name, iv := range map[string]training.Intervals{
		"EV lower limit 0":    {Days: bar.MinOOSDays, EV: &repo.Interval{Low: 0, High: 9}, AUC: &repo.Interval{Low: 0.52, High: 0.7}},
		"AUC lower limit 0.5": {Days: bar.MinOOSDays, EV: &repo.Interval{Low: 1, High: 9}, AUC: &repo.Interval{Low: 0.5, High: 0.7}},
	} {
		if status, _ := bar.Judge(e, iv); status != repo.ModelRejected {
			t.Errorf("%s: Judge = %s, want rejected", name, status)
		}
	}

	// A model that clears the bar but does not beat the model in force on the
	// same OOS rows is not adopted (the model learned from the same history).
	store := &memStore{}
	rows := noiseRows(12, rowsPerDay, 0.02, 0.25, edgeSeed)
	first, err := defaultBarTrainer(&fakeSource{rows: rows}, store).Train(ctx)
	if err != nil || first.Status != repo.ModelActive {
		t.Fatalf("first Train = %+v, %v, want adopted", first, err)
	}
	later := noiseRows(12, rowsPerDay, 0.02, 0.25, edgeSeed)
	for i := range later {
		later[i].DecidedAt = later[i].DecidedAt.AddDate(0, 0, 14)
	}
	second, err := defaultBarTrainer(&fakeSource{rows: later}, store, at(14), training.WithChangeLimits(changeguard.Limits{})).Train(ctx)
	if err != nil {
		t.Fatalf("second Train: %v", err)
	}
	if second.Status != repo.ModelRejected || second.RejectReason != training.RejectNotBetter {
		t.Errorf("second = %s (%s), want rejected: candidate_not_better_than_active", second.Status, second.RejectReason)
	}
}

// Issue #862: a retraining skipped because the split failed says why in one log
// line (the rows dropped as incomplete, the labeled decisions per label
// version, both sides of the split and what each needs) and records the same
// numbers; before, dropped_incomplete was only logged after a successful split.
func TestTrainer_LogsDroppedIncompleteWhenSplitFails(t *testing.T) {
	ctx := context.Background()
	logs := captureLogs(t)
	rec := &recordingSkips{}

	// 2 rows the features cannot be built from, 46 usable ones and 57 close-only
	// (label_version 1) decisions the source counts: far too little to train on
	// (the OOS window, which reaches back for 740 rows, takes all 46).
	rows := trainingRows(46, 3)
	rows = append(rows,
		trainingRowBroken(1), trainingRowBroken(2))
	src := &fakeSource{rows: rows, v1Rows: 57}
	tr := newTrainer(src, newStore(t), settingsOf(nil), training.WithSkipRecorder(rec))
	if err := tr.Retrain(ctx); err != nil {
		t.Fatalf("Retrain: %v", err)
	}

	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "nightly retraining skipped") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no skip line in the log:\n%s", logs.String())
	}
	t.Log(line)
	for _, want := range []string{"dropped_incomplete=2", "label_v1=57", "label_v2=48", "pre=0", "required_pre=130", "oos=46", "required_oos=740", "code=too_few_rows_before_oos"} {
		if !strings.Contains(line, want) {
			t.Errorf("skip line lacks %q: %s", want, line)
		}
	}

	if len(rec.got) != 1 {
		t.Fatalf("recorded %d skips, want 1", len(rec.got))
	}
	got := rec.got[0]
	if got.DroppedIncomplete != 2 || got.LabelV1Rows != 57 || got.LabelV2Rows != 48 ||
		got.UsableRows != 0 || got.RequiredRows != 130 || got.OOSRows != 46 || got.RequiredOOSRows != 740 {
		t.Errorf("recorded skip = %+v", got)
	}
}

// trainingRowBroken is a stored decision the features cannot be built from.
func trainingRowBroken(i int) repo.TrainingRow {
	return repo.TrainingRow{
		JevDecisionID: int64(9000 + i), DecidedAt: trainNow.Add(-48 * time.Hour), Direction: "LONG",
		StateJSON: `not json`, ResponseJSON: `{}`, HorizonMinutes: trainHorizon,
	}
}

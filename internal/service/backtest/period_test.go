package backtest_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// TestWalkForwardConfig_Splits_MultipleNonOverlappingFolds proves FR-BT-2's
// "全期間一括最適化しない" (do not evaluate the whole period in one shot):
// a range spanning several folds' worth of days produces more than one
// Split, each fold's Training/Validation/Forward periods contiguous and
// strictly chronological, and later folds strictly after earlier ones.
func TestWalkForwardConfig_Splits_MultipleNonOverlappingFolds(t *testing.T) {
	wf := backtest.WalkForwardConfig{
		Start:            mustParse(t, "2026-01-01T00:00:00Z"),
		End:              mustParse(t, "2026-01-16T00:00:00Z"),
		TrainingPeriod:   5 * 24 * time.Hour,
		ValidationPeriod: 2 * 24 * time.Hour,
		ForwardPeriod:    1 * 24 * time.Hour,
	}

	splits := wf.Splits()
	if len(splits) < 2 {
		t.Fatalf("expected multiple folds (not a single whole-period optimization), got %d: %+v", len(splits), splits)
	}

	for i, sp := range splits {
		if !sp.Training.End.Equal(sp.Validation.Start) {
			t.Errorf("split %d: Training.End=%s != Validation.Start=%s", i, sp.Training.End, sp.Validation.Start)
		}
		if !sp.Validation.End.Equal(sp.Forward.Start) {
			t.Errorf("split %d: Validation.End=%s != Forward.Start=%s", i, sp.Validation.End, sp.Forward.Start)
		}
		if sp.Forward.End.After(wf.End) {
			t.Errorf("split %d: Forward.End=%s exceeds wf.End=%s", i, sp.Forward.End, wf.End)
		}
		if i > 0 {
			prev := splits[i-1]
			if !sp.Training.Start.After(prev.Training.Start) {
				t.Errorf("split %d does not start strictly after split %d (no forward progress): %s vs %s",
					i, i-1, sp.Training.Start, prev.Training.Start)
			}
		}
	}
}

// TestWalkForwardConfig_Splits_TooShortRangeReturnsNil confirms a range
// smaller than one fold produces zero folds rather than silently
// collapsing into a single whole-period evaluation.
func TestWalkForwardConfig_Splits_TooShortRangeReturnsNil(t *testing.T) {
	wf := backtest.WalkForwardConfig{
		Start:            mustParse(t, "2026-01-01T00:00:00Z"),
		End:              mustParse(t, "2026-01-02T00:00:00Z"),
		TrainingPeriod:   5 * 24 * time.Hour,
		ValidationPeriod: 2 * 24 * time.Hour,
		ForwardPeriod:    1 * 24 * time.Hour,
	}

	if splits := wf.Splits(); splits != nil {
		t.Fatalf("expected nil splits for a too-short range, got %+v", splits)
	}
}

func TestPeriod_Contains(t *testing.T) {
	p := backtest.Period{
		Start: mustParse(t, "2026-01-01T00:00:00Z"),
		End:   mustParse(t, "2026-01-02T00:00:00Z"),
	}

	if !p.Contains(p.Start) {
		t.Error("Contains(Start) = false, want true (half-open range includes Start)")
	}
	if p.Contains(p.End) {
		t.Error("Contains(End) = true, want false (half-open range excludes End)")
	}
	if !p.Contains(p.Start.Add(time.Hour)) {
		t.Error("Contains(Start+1h) = false, want true")
	}
}

// SplitCount must agree with len(Splits()) - it is what bounds a request's
// cost before Splits() allocates - including the default step, a custom
// step, exact fit, one short of fit, and invalid periods.
func TestWalkForwardConfig_SplitCount_MatchesSplits(t *testing.T) {
	const day = 24 * time.Hour
	start := mustParse(t, "2026-01-01T00:00:00Z")
	for name, wf := range map[string]backtest.WalkForwardConfig{
		"default step":      {Start: start, End: start.Add(20 * day), TrainingPeriod: 5 * day, ValidationPeriod: 2 * day, ForwardPeriod: day},
		"custom step":       {Start: start, End: start.Add(20 * day), TrainingPeriod: 5 * day, ValidationPeriod: 2 * day, ForwardPeriod: day, StepPeriod: 3 * day},
		"exact single fold": {Start: start, End: start.Add(8 * day), TrainingPeriod: 5 * day, ValidationPeriod: 2 * day, ForwardPeriod: day},
		"one short of fold": {Start: start, End: start.Add(8*day - time.Nanosecond), TrainingPeriod: 5 * day, ValidationPeriod: 2 * day, ForwardPeriod: day},
		"non-positive":      {Start: start, End: start.Add(20 * day), TrainingPeriod: 5 * day, ValidationPeriod: 0, ForwardPeriod: day},
	} {
		t.Run(name, func(t *testing.T) {
			if got, want := wf.SplitCount(), len(wf.Splits()); got != want {
				t.Errorf("SplitCount() = %d, want len(Splits()) = %d", got, want)
			}
		})
	}
}

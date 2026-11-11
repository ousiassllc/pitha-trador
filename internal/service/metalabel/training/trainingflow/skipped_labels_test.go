package trainingflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// The nightly retraining reads the rows it always has while
// metalabel.include_skipped_labels is off (the default): the skipped-candidate
// listing is never called, and the stored model is the one trained on those
// rows alone.
func TestTrainer_IgnoresSkippedCandidateRowsByDefault(t *testing.T) {
	skipped := trainingRows(50, 9)
	for i := range skipped {
		skipped[i].JevDecisionID += 100000
		skipped[i].SkippedLabel = true
	}
	src := &fakeSource{rows: trainingRows(3000, 1), skipped: skipped}
	saved, err := newTrainer(src, newStore(t), settingsOf(nil)).Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if src.withSkipped != 0 {
		t.Fatalf("the skipped-candidate listing was read %d times with the switch off, want 0", src.withSkipped)
	}

	// Same rows, no skipped candidates at all: an identical model.
	plain := &fakeSource{rows: trainingRows(3000, 1)}
	want, err := newTrainer(plain, newStore(t), settingsOf(nil)).Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if saved.TrainSamples != want.TrainSamples || saved.OOSSamples != want.OOSSamples || string(saved.ParamsJSON) != string(want.ParamsJSON) {
		t.Errorf("a source with skipped rows changed the model with the switch off: %d/%d samples vs %d/%d", saved.TrainSamples, saved.OOSSamples, want.TrainSamples, want.OOSSamples)
	}
}

// With the switch on the retraining reads the listing that adds them.
func TestTrainer_ReadsSkippedCandidateRowsOnlyWhenTheSwitchIsOn(t *testing.T) {
	skipped := trainingRows(400, 9)
	for i := range skipped {
		skipped[i].JevDecisionID += 100000
		skipped[i].SkippedLabel = true
		skipped[i].DecidedAt = skipped[i].DecidedAt.Add(-1)
	}
	src := &fakeSource{rows: trainingRows(3000, 1), skipped: skipped}
	logs := captureLogs(t)
	saved, err := newTrainer(src, newStore(t), settingsOf(func(s *metalabelsettings.Settings) { s.IncludeSkippedLabels = true })).Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if src.withSkipped != 1 {
		t.Fatalf("the skipped-candidate listing was read %d times with the switch on, want 1", src.withSkipped)
	}
	if saved.TrainSamples+saved.OOSSamples < 3000 {
		t.Errorf("train+OOS samples = %d, want the added rows to count", saved.TrainSamples+saved.OOSSamples)
	}
	if !strings.Contains(logs.String(), "metalabel.include_skipped_labels") {
		t.Errorf("the log does not say the retraining mixes in skipped candidates:\n%s", logs.String())
	}
}

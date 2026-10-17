package trainingflow_test

import (
	"context"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

const edgeSeed = 9000

// edgeRows is a dataset a trained model adopts under the default bar.
func edgeRows() []repo.TrainingRow { return noiseRows(8, 360, 0.02, 0.25, edgeSeed) }

// edgeRowsLater is the same history moved by whole days, for a later run.
func edgeRowsLater(days int) []repo.TrainingRow {
	rows := edgeRows()
	for i := range rows {
		rows[i].DecidedAt = rows[i].DecidedAt.AddDate(0, 0, days)
	}
	return rows
}

// at is a training clock option running the trainer days after trainNow.
func at(days int) training.TrainerOption {
	return training.WithClock(func() time.Time { return trainNow.AddDate(0, 0, days) })
}

// weakActiveModel stores an adopted model without skill (trained on edge-free
// data), trained at, as the model in force a candidate is compared with.
func weakActiveModel(t *testing.T, store *memStore, trainedAt time.Time) repo.Model {
	t.Helper()
	m, err := newTrainer(&fakeSource{rows: noiseRows(8, 360, 0.02, 0, 77)}, &memStore{}, settingsOf(nil)).Train(context.Background())
	if err != nil {
		t.Fatalf("train the weak model: %v", err)
	}
	m.Status, m.RejectReason, m.TrainedAt = repo.ModelActive, "", trainedAt
	saved, err := store.SaveModel(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func defaultBarTrainer(src training.TrainingSource, store training.ModelStore, opts ...training.TrainerOption) *training.Trainer {
	return newTrainer(src, store, settingsOf(nil), append([]training.TrainerOption{
		training.WithAcceptance(training.DefaultAcceptanceConfig()), training.WithChangeLimits(changeguard.DefaultLimits()),
	}, opts...)...)
}

type fakePolicyChanges struct {
	at    time.Time
	found bool
}

func (f fakePolicyChanges) LastChangeAt(context.Context) (time.Time, bool, error) {
	return f.at, f.found, nil
}

// A candidate that clears the bar replaces a model without skill once the rate
// limit allows it, and the comparison and criteria are recorded with it.
func TestAdoption_CandidateBeatingTheActiveModelIsAdoptedAndRecorded(t *testing.T) {
	store := &memStore{}
	weak := weakActiveModel(t, store, trainNow.Add(-10*24*time.Hour))
	got, err := defaultBarTrainer(&fakeSource{rows: edgeRows()}, store).Train(context.Background())
	if err != nil || got.Status != repo.ModelActive {
		t.Fatalf("Train = %+v, %v, want the candidate adopted over %s", got, err, weak.Version)
	}
	cmp := got.Adoption.Comparison
	if cmp == nil || !cmp.Comparable || cmp.ActiveVersion != weak.Version || cmp.DiffBps <= 0 || cmp.DiffCI == nil || cmp.DiffCI.Low <= 0 {
		t.Errorf("comparison = %+v, want a significant improvement over the weak model", cmp)
	}
	crit := training.DefaultAcceptanceConfig()
	if got.Adoption.Criteria.MinTakes != crit.MinTakes || got.Adoption.Criteria.MinOOSSamples != crit.MinOOSSamples || got.Adoption.Criteria.Resamples != crit.Resamples {
		t.Errorf("recorded criteria = %+v, want the defaults", got.Adoption.Criteria)
	}
	if got.OOSTakes < crit.MinTakes || got.OOSDays < crit.MinOOSDays || got.OOSEVCI == nil || got.OOSEVCI.Low <= 0 || got.OOSAUCCI == nil || got.OOSAUCCI.Low <= 0.5 {
		t.Errorf("recorded metrics: takes %d days %d ev ci %+v auc ci %+v", got.OOSTakes, got.OOSDays, got.OOSEVCI, got.OOSAUCCI)
	}
}

// A model that clears the bar is still not adopted when the model in force is
// not beaten on the same OOS rows: here the candidate is learned from the same
// data as the active model (no significant difference), so the active model stays.
func TestAdoption_CandidateNotBetterThanTheActiveModelIsNotAdopted(t *testing.T) {
	store := &memStore{}
	ctx := context.Background()
	first, err := defaultBarTrainer(&fakeSource{rows: edgeRows()}, store).Train(ctx)
	if err != nil || first.Status != repo.ModelActive {
		t.Fatalf("first Train = %+v, %v", first, err)
	}
	// Ten trading days later the same history is retrained: the candidate clears the
	// bar itself but is no better than the model in force.
	second, err := defaultBarTrainer(&fakeSource{rows: edgeRowsLater(14)}, store, at(14)).Train(ctx)
	if err != nil {
		t.Fatalf("second Train: %v", err)
	}
	if second.Status != repo.ModelRejected || second.RejectReason != training.RejectNotBetter {
		t.Fatalf("second = %q (%s), want rejected: not better than the active model", second.Status, second.RejectReason)
	}
	if cmp := second.Adoption.Comparison; cmp == nil || cmp.ActiveVersion != first.Version || cmp.DiffCI == nil || cmp.DiffCI.Low > 0 {
		t.Errorf("comparison = %+v", cmp)
	}
	if got, _, _ := store.LatestActiveModel(ctx); got.Version != first.Version {
		t.Errorf("active = %s, want %s to stay in force", got.Version, first.Version)
	}
}

// Rate limit: a second adoption inside the window is refused, with the reason,
// the detail and the end of the block recorded; the model in force is untouched.
func TestAdoption_RateLimitBlocksASecondSwapInTheWindowAndRecordsWhy(t *testing.T) {
	store := &memStore{}
	ctx := context.Background()
	// The model in force was adopted 1 trading day before trainNow (a Wednesday).
	weak := weakActiveModel(t, store, trainNow.Add(-24*time.Hour))

	blocked, err := defaultBarTrainer(&fakeSource{rows: edgeRows()}, store).Train(ctx)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if blocked.Status != repo.ModelRejected || blocked.RejectReason != changeguard.ReasonModelSwapRateLimited {
		t.Fatalf("model = %q (%s), want rejected by the model-swap rate limit", blocked.Status, blocked.RejectReason)
	}
	gate := blocked.Adoption.Gate
	if gate == nil || gate.Reason != changeguard.ReasonModelSwapRateLimited || gate.Detail == "" || gate.Until == "" {
		t.Errorf("gate = %+v, want the reason, detail and end of the block recorded", gate)
	}
	if blocked.Adoption.Comparison == nil {
		t.Error("the comparison was not recorded for the blocked candidate")
	}
	if got, _, _ := store.LatestActiveModel(ctx); got.Version != weak.Version {
		t.Errorf("active = %s, want %s untouched", got.Version, weak.Version)
	}

	// Past the window (the next attempt a week later) the same candidate is adopted.
	later, err := defaultBarTrainer(&fakeSource{rows: edgeRowsLater(7)}, store, at(7)).Train(ctx)
	if err != nil || later.Status != repo.ModelActive || later.Adoption.Gate != nil {
		t.Fatalf("after the window: %+v, %v, want adopted", later, err)
	}
}

// Freeze: no model is adopted shortly after the self-improvement flow changed
// policy.* thresholds, so the two changes never land in one window.
func TestAdoption_FreezeAfterAPolicyChangeBlocksTheSwapAndRecordsWhy(t *testing.T) {
	store := &memStore{}
	ctx := context.Background()
	changed := trainNow.Add(-26 * time.Hour)
	blocked, err := defaultBarTrainer(&fakeSource{rows: edgeRows()}, store, training.WithPolicyChanges(fakePolicyChanges{at: changed, found: true})).Train(ctx)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if blocked.Status != repo.ModelRejected || blocked.RejectReason != changeguard.ReasonPolicyChangeFreeze || blocked.Adoption.Gate == nil {
		t.Fatalf("model = %q (%s) gate %+v, want rejected by the post-policy-change freeze", blocked.Status, blocked.RejectReason, blocked.Adoption.Gate)
	}
	if _, found, _ := store.LatestActiveModel(ctx); found {
		t.Error("a model is in force although the adoption was frozen")
	}

	adopted, err := defaultBarTrainer(&fakeSource{rows: edgeRowsLater(4)}, store, training.WithPolicyChanges(fakePolicyChanges{at: changed, found: true}), at(4)).Train(ctx)
	if err != nil || adopted.Status != repo.ModelActive {
		t.Fatalf("after the freeze: %+v, %v, want adopted", adopted, err)
	}
}

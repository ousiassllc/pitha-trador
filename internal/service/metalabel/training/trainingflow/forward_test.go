package trainingflow_test

import (
	"context"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// The forward (holdout) evaluation of the adopted model covers only decisions
// made after it was trained, is stored without touching the adoption record
// and never changes which model is in force.
func TestRetrain_StoresTheForwardEvaluationOfTheActiveModelOnly(t *testing.T) {
	store := &memStore{}
	ctx := context.Background()
	adopted, err := defaultBarTrainer(&fakeSource{rows: edgeRows()}, store).Train(ctx)
	if err != nil || adopted.Status != repo.ModelActive {
		t.Fatalf("Train = %+v, %v", adopted, err)
	}
	// Four trading days later the history has grown by decisions the model never saw; the
	// retraining of that night is rejected (the rate limit) but the active model is still
	// evaluated forward, on the new decisions only.
	later := edgeRowsLater(4)
	if err := defaultBarTrainer(&fakeSource{rows: later}, store, at(4)).Retrain(ctx); err != nil {
		t.Fatalf("Retrain: %v", err)
	}
	var active repo.Model
	for _, m := range store.models {
		if m.ID == adopted.ID {
			active = m
		}
	}
	f := active.Forward
	if f.EvaluatedAt == "" || f.From != adopted.TrainedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("Forward = %+v, want an evaluation from the model's trained_at", f)
	}
	var want int
	for _, r := range later {
		if !r.DecidedAt.Before(adopted.TrainedAt) && !r.DecidedAt.Add(time.Duration(r.HorizonMinutes)*time.Minute).After(trainNow.AddDate(0, 0, 4)) {
			want++
		}
	}
	if f.Samples != want || f.Samples == 0 || f.Days == 0 || f.Takes == 0 {
		t.Errorf("Forward samples %d takes %d days %d, want %d samples after trained_at", f.Samples, f.Takes, f.Days, want)
	}
	if active.Status != repo.ModelActive || active.Adoption.Criteria.MinTakes != adopted.Adoption.Criteria.MinTakes {
		t.Errorf("the forward evaluation changed the adoption record: %+v", active)
	}
	if got, _, _ := store.LatestActiveModel(ctx); got.ID != adopted.ID {
		t.Errorf("active = %d, want %d to stay in force", got.ID, adopted.ID)
	}
}

func TestRetrain_WithoutAnActiveModelStoresNoForwardEvaluation(t *testing.T) {
	store := &memStore{}
	if err := defaultBarTrainer(&fakeSource{rows: noiseRows(8, 120, 0.02, 0, 3)}, store).Retrain(context.Background()); err != nil {
		t.Fatalf("Retrain: %v", err)
	}
	for _, m := range store.models {
		if m.Forward.EvaluatedAt != "" {
			t.Errorf("model %d has a forward evaluation without being adopted: %+v", m.ID, m.Forward)
		}
	}
}

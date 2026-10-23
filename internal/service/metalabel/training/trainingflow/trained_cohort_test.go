package trainingflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// Issue #881: a trained model records the Trader prompt cohort its rows came
// from (in adoption_json, no migration) and ParseModel hands it to Policy.
func TestTrain_RecordsTheCohortOfTheLiveVersionOnTheModel(t *testing.T) {
	store := &memStore{}
	cohort := func(context.Context) (string, error) { return "entry-quality-v4", nil }
	got, err := newTrainer(&fakeSource{rows: edgeRows()}, store, settingsOf(nil), training.WithTrainedCohort(cohort)).Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if got.Adoption.TrainedCohort != "entry-quality-v4" {
		t.Fatalf("Adoption.TrainedCohort = %q, want entry-quality-v4", got.Adoption.TrainedCohort)
	}
	got.Status = "active"
	parsed, err := metalabel.ParseModel(got)
	if err != nil || parsed.TrainedCohort != "entry-quality-v4" {
		t.Fatalf("ParseModel = %+v, %v, want TrainedCohort entry-quality-v4", parsed, err)
	}
}

// Without the option nothing is recorded (a model with no recorded cohort).
func TestTrain_WithoutACohortSourceRecordsNone(t *testing.T) {
	got, err := newTrainer(&fakeSource{rows: edgeRows()}, &memStore{}, settingsOf(nil)).Train(context.Background())
	if err != nil || got.Adoption.TrainedCohort != "" {
		t.Fatalf("Train = %q, %v, want no recorded cohort", got.Adoption.TrainedCohort, err)
	}
}

// A live version that cannot be read, or that is promoted into another cohort
// while the rows are read, fails the training: a model is never labeled with a
// cohort other than the one its rows came from, and nothing is stored.
func TestTrain_FailsWhenTheLiveCohortCannotBeReadOrChangesMidRead(t *testing.T) {
	store := &memStore{}
	failing := func(context.Context) (string, error) { return "", errors.New("settings unreadable") }
	if _, err := newTrainer(&fakeSource{rows: edgeRows()}, store, settingsOf(nil), training.WithTrainedCohort(failing)).Train(context.Background()); err == nil {
		t.Fatal("Train with an unreadable live cohort: want an error")
	}

	reads := 0
	promoted := func(context.Context) (string, error) {
		reads++
		if reads == 1 {
			return "entry-quality-v3", nil
		}
		return "entry-quality-v4", nil
	}
	_, err := newTrainer(&fakeSource{rows: edgeRows()}, store, settingsOf(nil), training.WithTrainedCohort(promoted)).Train(context.Background())
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Train across a promotion = %v, want a cohort-changed error", err)
	}
	if len(store.models) != 0 {
		t.Fatalf("stored %d models, want none", len(store.models))
	}
}

package metahistory_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/metahistory"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func day(d int) time.Time { return time.Date(2026, 10, d, 18, 0, 0, 0, time.UTC) }

func stored(t *testing.T, id int64, version string, trainedAt time.Time, status string) repo.Model {
	t.Helper()
	model := policytest.TakingModel()
	params, err := model.ParamsJSON()
	if err != nil {
		t.Fatalf("ParamsJSON: %v", err)
	}
	return repo.Model{
		ID: id, Version: version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: status, TrainedAt: trainedAt, FeatureNames: model.FeatureNames, ParamsJSON: params,
	}
}

func TestTimeline_ModelAtUsesOnlyModelsTrainedBeforeTheDecision(t *testing.T) {
	tl := metahistory.New([]repo.Model{
		stored(t, 2, "v2", day(14), repo.ModelActive),
		stored(t, 1, "v1", day(10), repo.ModelActive),
	})

	if m, err := tl.ModelAt(day(10).Add(-time.Second)); m != nil || err != nil {
		t.Fatalf("before the first model: (%v, %v), want (nil, nil): no model existed yet", m, err)
	}
	if m, err := tl.ModelAt(day(10)); m != nil || err != nil {
		t.Fatalf("at the first model's training instant: (%v, %v), want (nil, nil): trained strictly before is required", m, err)
	}
	if m, err := tl.ModelAt(day(12)); err != nil || m == nil || m.Version != "v1" || m.ID != 1 {
		t.Fatalf("between the models: (%+v, %v), want v1", m, err)
	}
	if m, err := tl.ModelAt(day(14).Add(time.Nanosecond)); err != nil || m == nil || m.Version != "v2" {
		t.Fatalf("after the second model: (%+v, %v), want v2 (the newest trained before)", m, err)
	}
	if m, _ := tl.ModelAt(day(30)); m == nil || m.Version != "v2" {
		t.Fatalf("long after: %+v, want v2", m)
	}
}

func TestTimeline_RejectedModelsWereNeverInForce(t *testing.T) {
	tl := metahistory.New([]repo.Model{
		stored(t, 1, "v1", day(10), repo.ModelActive),
		stored(t, 2, "v2-rejected", day(12), repo.ModelRejected),
	})
	if tl.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (only the active model)", tl.Len())
	}
	if m, err := tl.ModelAt(day(20)); err != nil || m == nil || m.Version != "v1" {
		t.Fatalf("ModelAt = (%+v, %v), want v1: a rejected newer model does not replace it", m, err)
	}
}

func TestTimeline_UnusableNewestModelIsAnErrorNotAFallback(t *testing.T) {
	broken := stored(t, 2, "v2", day(14), repo.ModelActive)
	broken.ParamsJSON = `{"not":"a model"}`
	tl := metahistory.New([]repo.Model{stored(t, 1, "v1", day(10), repo.ModelActive), broken})

	if m, err := tl.ModelAt(day(20)); m != nil || err == nil {
		t.Fatalf("ModelAt = (%+v, %v), want an error: live does not fall back to an older model either", m, err)
	}
	if m, err := tl.ModelAt(day(12)); err != nil || m == nil || m.Version != "v1" {
		t.Fatalf("before the broken model: (%+v, %v), want v1", m, err)
	}
}

func TestTimeline_EmptyHistoryHasNoModel(t *testing.T) {
	if m, err := metahistory.New(nil).ModelAt(day(20)); m != nil || err != nil {
		t.Fatalf("ModelAt = (%v, %v), want (nil, nil)", m, err)
	}
}

package metalabel_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func sampleModel(version, status string, trainedAt time.Time) metalabel.Model {
	auc, ev := 0.61, 3.5
	return metalabel.Model{
		Version: version, Algorithm: "logistic_regression", LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: status, TrainedAt: trainedAt,
		TrainStart: trainedAt.AddDate(0, 0, -60), TrainEnd: trainedAt.AddDate(0, 0, -10),
		OOSStart: trainedAt.AddDate(0, 0, -9), OOSEnd: trainedAt.AddDate(0, 0, -1),
		TrainSamples: 400, OOSSamples: 80,
		FeatureNames: []string{"ofi_short", "spread_bps"}, ParamsJSON: `{"w":[0.1,-0.2]}`,
		OOSAUC: &auc, OOSExpectedValueBps: &ev,
		OOSCalibration: []metalabel.CalibrationBin{{MeanPredicted: 0.55, ObservedRate: 0.5, Count: 40}},
	}
}

func TestRepository_InsertModelRoundTripsVersionAndOOSMetrics(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	want := sampleModel("20261014-logreg", metalabel.ModelActive, time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC))

	saved, err := r.InsertModel(ctx, want)
	if err != nil {
		t.Fatalf("InsertModel: %v", err)
	}
	if saved.ID == 0 || saved.CreatedAt.IsZero() {
		t.Fatalf("saved ID/CreatedAt = %d/%v, want both set", saved.ID, saved.CreatedAt)
	}
	got, found, err := r.LatestActiveModel(ctx)
	if err != nil || !found {
		t.Fatalf("LatestActiveModel = found %v, err %v", found, err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Errorf("round trip = %+v, want %+v", got, saved)
	}
}

func TestRepository_NullOOSMetricsRoundTripAsNil(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	m := sampleModel("v-null", metalabel.ModelActive, time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC))
	m.OOSAUC, m.OOSExpectedValueBps, m.FeatureNames, m.OOSCalibration = nil, nil, nil, nil
	if _, err := r.InsertModel(ctx, m); err != nil {
		t.Fatalf("InsertModel: %v", err)
	}
	got, found, err := r.LatestActiveModel(ctx)
	if err != nil || !found {
		t.Fatalf("LatestActiveModel = found %v, err %v", found, err)
	}
	if got.OOSAUC != nil || got.OOSExpectedValueBps != nil {
		t.Errorf("OOS metrics = %v/%v, want nil/nil", got.OOSAUC, got.OOSExpectedValueBps)
	}
	if len(got.FeatureNames) != 0 || len(got.OOSCalibration) != 0 {
		t.Errorf("feature names/curve = %v/%v, want empty", got.FeatureNames, got.OOSCalibration)
	}
}

func TestRepository_LatestActiveModelSkipsRejectedAndPicksNewest(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)

	if _, found, err := r.LatestActiveModel(ctx); err != nil || found {
		t.Fatalf("empty store: found %v, err %v, want not found", found, err)
	}
	for _, m := range []metalabel.Model{
		sampleModel("old-active", metalabel.ModelActive, base),
		sampleModel("newest-rejected", metalabel.ModelRejected, base.AddDate(0, 0, 2)),
		sampleModel("new-active", metalabel.ModelActive, base.AddDate(0, 0, 1)),
	} {
		if _, err := r.InsertModel(ctx, m); err != nil {
			t.Fatalf("InsertModel %s: %v", m.Version, err)
		}
	}
	got, found, err := r.LatestActiveModel(ctx)
	if err != nil || !found {
		t.Fatalf("LatestActiveModel = found %v, err %v", found, err)
	}
	if got.Version != "new-active" {
		t.Errorf("latest active = %q, want new-active", got.Version)
	}
}

func TestRepository_InsertModelRejectsDuplicateVersion(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	m := sampleModel("dup", metalabel.ModelActive, time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC))
	if _, err := r.InsertModel(ctx, m); err != nil {
		t.Fatalf("first InsertModel: %v", err)
	}
	if _, err := r.InsertModel(ctx, m); err == nil {
		t.Fatal("second InsertModel with the same version succeeded, want error")
	}
}

func TestRepository_PredictionRoundTripsPerDecision(t *testing.T) {
	conn := newTestDB(t)
	r := metalabel.NewRepository(conn)
	ctx := context.Background()
	model, err := r.InsertModel(ctx, sampleModel("v1", metalabel.ModelActive, time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("InsertModel: %v", err)
	}
	d1, d2 := insertDecision(t, conn, "7203"), insertDecision(t, conn, "6758")

	if _, found, err := r.PredictionForDecision(ctx, model.ID, d1); err != nil || found {
		t.Fatalf("before insert: found %v, err %v, want not found", found, err)
	}
	take := metalabel.Prediction{
		ModelID: model.ID, JevDecisionID: d1, PredictedAt: time.Date(2026, 10, 15, 9, 5, 0, 0, time.UTC),
		RawProbability: 0.58, CalibratedProbability: 0.55, ExpectedValueBps: 4.2,
		Action: metalabel.ActionTake, SizeFraction: 0.5, Reason: "ev>0", FeaturesJSON: `{"ofi_short":1.5}`,
	}
	skip := metalabel.Prediction{
		ModelID: model.ID, JevDecisionID: d2, PredictedAt: take.PredictedAt,
		RawProbability: 0.4, CalibratedProbability: 0.42, ExpectedValueBps: -2,
		Action: metalabel.ActionSkip, Reason: "ev<=0", FeaturesJSON: `{}`,
	}
	savedTake, err := r.InsertPrediction(ctx, take)
	if err != nil {
		t.Fatalf("InsertPrediction take: %v", err)
	}
	if _, err := r.InsertPrediction(ctx, skip); err != nil {
		t.Fatalf("InsertPrediction skip: %v", err)
	}

	got, found, err := r.PredictionForDecision(ctx, model.ID, d1)
	if err != nil || !found {
		t.Fatalf("PredictionForDecision = found %v, err %v", found, err)
	}
	if !reflect.DeepEqual(got, savedTake) {
		t.Errorf("round trip = %+v, want %+v", got, savedTake)
	}
	gotSkip, _, _ := r.PredictionForDecision(ctx, model.ID, d2)
	if gotSkip.Action != metalabel.ActionSkip || gotSkip.SizeFraction != 0 {
		t.Errorf("skip prediction = %+v, want action skip, size 0", gotSkip)
	}
	if _, err := r.InsertPrediction(ctx, take); err == nil {
		t.Error("second prediction of the same model and decision succeeded, want error")
	}
}

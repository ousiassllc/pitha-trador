package metalabel_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

func newStore(t *testing.T) (*metalabel.Store, func() int64) {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	n := 0
	newDecision := func() int64 {
		n++
		ctx := context.Background()
		res, err := conn.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, 'x', 'TSE Prime', 1)`, 1000+n)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		instID, _ := res.LastInsertId()
		res, err = conn.ExecContext(ctx, `INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id, created_at)
			VALUES (?, 'S', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm', 't')`, instID)
		if err != nil {
			t.Fatalf("insert decision: %v", err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	return metalabel.NewStore(repo.NewRepository(conn)), newDecision
}

func validModel(version string) repo.Model {
	trained := time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)
	auc := 0.6
	return repo.Model{
		Version: version, Algorithm: "logistic_regression", LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: repo.ModelActive, TrainedAt: trained,
		TrainStart: trained.AddDate(0, 0, -60), TrainEnd: trained.AddDate(0, 0, -10),
		OOSStart: trained.AddDate(0, 0, -9), OOSEnd: trained.AddDate(0, 0, -1),
		TrainSamples: 300, OOSSamples: 60, FeatureNames: []string{"ofi_short"}, ParamsJSON: `{}`, OOSAUC: &auc,
	}
}

func validPrediction(modelID, decisionID int64) repo.Prediction {
	return repo.Prediction{
		ModelID: modelID, JevDecisionID: decisionID, PredictedAt: time.Date(2026, 10, 15, 9, 5, 0, 0, time.UTC),
		RawProbability: 0.6, CalibratedProbability: 0.57, ExpectedValueBps: 3,
		Action: repo.ActionTake, SizeFraction: 0.5, FeaturesJSON: `{}`,
	}
}

func TestStore_SavedModelIsReturnedAsLatestActiveWithOOSMetrics(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, found, err := s.LatestActiveModel(ctx); err != nil || found {
		t.Fatalf("no model saved: found %v, err %v, want not found (caller must skip)", found, err)
	}
	if _, err := s.SaveModel(ctx, validModel("v1")); err != nil {
		t.Fatalf("SaveModel v1: %v", err)
	}
	rejected := validModel("v2-rejected")
	rejected.Status, rejected.RejectReason = repo.ModelRejected, "OOS EV <= 0"
	rejected.TrainedAt = rejected.TrainedAt.AddDate(0, 0, 1)
	if _, err := s.SaveModel(ctx, rejected); err != nil {
		t.Fatalf("SaveModel rejected: %v", err)
	}

	got, found, err := s.LatestActiveModel(ctx)
	if err != nil || !found {
		t.Fatalf("LatestActiveModel = found %v, err %v", found, err)
	}
	if got.Version != "v1" || got.OOSAUC == nil || *got.OOSAUC != 0.6 || got.OOSSamples != 60 {
		t.Errorf("latest active = %+v, want v1 with OOS AUC 0.6 and 60 OOS samples", got)
	}
}

func TestStore_SaveModelRejectsInvalidModels(t *testing.T) {
	s, _ := newStore(t)
	badAUC := 1.5
	nan := math.NaN()
	tests := []struct {
		name   string
		mutate func(*repo.Model)
	}{
		{"empty version", func(m *repo.Model) { m.Version = "" }},
		{"empty algorithm", func(m *repo.Model) { m.Algorithm = "" }},
		{"unknown status", func(m *repo.Model) { m.Status = "bogus" }},
		{"missing OOS window", func(m *repo.Model) { m.OOSStart, m.OOSEnd = time.Time{}, time.Time{} }},
		{"OOS ends before it starts", func(m *repo.Model) { m.OOSEnd = m.OOSStart.Add(-time.Hour) }},
		{"OOS overlaps training", func(m *repo.Model) { m.OOSStart = m.TrainEnd.Add(-time.Hour) }},
		{"AUC above 1", func(m *repo.Model) { m.OOSAUC = &badAUC }},
		{"NaN AUC", func(m *repo.Model) { m.OOSAUC = &nan }},
		{"NaN EV", func(m *repo.Model) { m.OOSExpectedValueBps = &nan }},
		{"empty params", func(m *repo.Model) { m.ParamsJSON = "" }},
		{"negative samples", func(m *repo.Model) { m.OOSSamples = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := validModel("v-" + tc.name)
			tc.mutate(&m)
			if _, err := s.SaveModel(context.Background(), m); !errors.Is(err, metalabel.ErrInvalid) {
				t.Errorf("SaveModel error = %v, want ErrInvalid", err)
			}
		})
	}
	if _, found, _ := s.LatestActiveModel(context.Background()); found {
		t.Error("an invalid model was stored")
	}
}

func TestStore_RecordPredictionPersistsPerDecision(t *testing.T) {
	s, newDecision := newStore(t)
	ctx := context.Background()
	model, err := s.SaveModel(ctx, validModel("v1"))
	if err != nil {
		t.Fatalf("SaveModel: %v", err)
	}
	decision := newDecision()

	saved, err := s.RecordPrediction(ctx, validPrediction(model.ID, decision))
	if err != nil {
		t.Fatalf("RecordPrediction: %v", err)
	}
	got, found, err := s.PredictionForDecision(ctx, model.ID, decision)
	if err != nil || !found {
		t.Fatalf("PredictionForDecision = found %v, err %v", found, err)
	}
	if got.ID != saved.ID || got.CalibratedProbability != 0.57 || got.ExpectedValueBps != 3 || got.Action != repo.ActionTake {
		t.Errorf("stored prediction = %+v, want the recorded take", got)
	}
	if _, err := s.RecordPrediction(ctx, validPrediction(model.ID, decision)); err == nil {
		t.Error("recording the same model/decision twice succeeded, want error")
	}
}

func TestStore_RecordPredictionRejectsInvalidPredictions(t *testing.T) {
	s, _ := newStore(t)
	nan := math.NaN()
	tests := []struct {
		name   string
		mutate func(*repo.Prediction)
	}{
		{"no model", func(p *repo.Prediction) { p.ModelID = 0 }},
		{"no decision", func(p *repo.Prediction) { p.JevDecisionID = 0 }},
		{"no predicted_at", func(p *repo.Prediction) { p.PredictedAt = time.Time{} }},
		{"probability above 1", func(p *repo.Prediction) { p.CalibratedProbability = 1.1 }},
		{"NaN raw probability", func(p *repo.Prediction) { p.RawProbability = nan }},
		{"infinite EV", func(p *repo.Prediction) { p.ExpectedValueBps = math.Inf(1) }},
		{"unknown action", func(p *repo.Prediction) { p.Action = "hold" }},
		{"size above 1", func(p *repo.Prediction) { p.SizeFraction = 1.5 }},
		{"skip with size", func(p *repo.Prediction) { p.Action = repo.ActionSkip }},
		{"take without size", func(p *repo.Prediction) { p.SizeFraction = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validPrediction(1, 1)
			tc.mutate(&p)
			if _, err := s.RecordPrediction(context.Background(), p); !errors.Is(err, metalabel.ErrInvalid) {
				t.Errorf("RecordPrediction error = %v, want ErrInvalid", err)
			}
		})
	}
}

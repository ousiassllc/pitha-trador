// Package policytest provides the test doubles of the meta-labeling entry rule
// (issue #765): scripted models, a prediction recorder and the model inputs of
// a decision, so tests of Engine, Handler and their callers need not train a
// model or hand-write feature vectors.
package policytest

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// LRModel returns a meta-labeling model that scores bias + weights . features
// through an identity (Platt A=1, B=0) calibration, so the calibrated
// probability is sigmoid(bias + weights . features). A win pays 30 bps and a
// loss costs 20, so the expected value is positive above p = 0.4. Its ID is 11
// and its version "lr-test".
func LRModel(bias float64, weights ...float64) metalabel.Model {
	n := len(metalabel.FeatureNames())
	w := make([]float64, n)
	copy(w, weights)
	std := make([]float64, n)
	for i := range std {
		std[i] = 1
	}
	return metalabel.Model{
		ID: 11, Version: "lr-test", FeatureNames: metalabel.FeatureNames(),
		LR:         metalabel.LogisticRegression{Mean: make([]float64, n), Std: std, Weights: w, Bias: bias},
		Calibrator: metalabel.Calibrator{Method: metalabel.CalibrationPlatt, A: 1},
		WinBps:     30, LossBps: 20,
	}
}

// TakingModel scores every candidate at p = 0.88 (EV +24 bps); SkippingModel at
// p = 0.12 (EV -14 bps).
func TakingModel() metalabel.Model   { return LRModel(2) }
func SkippingModel() metalabel.Model { return LRModel(-2) }

// Models is a policy.MetaModelSource returning Model (nil: no active model) or
// Err.
type Models struct {
	Model *metalabel.Model
	Err   error
}

// ActiveModel implements policy.MetaModelSource.
func (m Models) ActiveModel(context.Context) (*metalabel.Model, error) { return m.Model, m.Err }

// ModelOf returns the Models source of model.
func ModelOf(model metalabel.Model) Models { return Models{Model: &model} }

// Predictions is a policy.PredictionRecorder keeping what it was given; Err
// makes it fail.
type Predictions struct {
	mu   sync.Mutex
	rows []repo.Prediction
	Err  error
}

// RecordPrediction implements policy.PredictionRecorder.
func (r *Predictions) RecordPrediction(_ context.Context, p repo.Prediction) (repo.Prediction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return repo.Prediction{}, r.Err
	}
	r.rows = append(r.rows, p)
	return p, nil
}

// Recorded returns the predictions recorded so far.
func (r *Predictions) Recorded() []repo.Prediction {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]repo.Prediction(nil), r.rows...)
}

// DefaultConfig is the take/skip policy of metalabel.DefaultDecideConfig.
func DefaultConfig(context.Context) (metalabel.DecideConfig, error) {
	return metalabel.DefaultDecideConfig(), nil
}

// Option wires models and predictions with the default take/skip policy.
func Option(models policy.MetaModelSource, predictions policy.PredictionRecorder) policy.Option {
	return policy.WithMetaModel(models, predictions, DefaultConfig)
}

// NewStore returns a Store over db holding model as its active model: a real
// Store both loads the model (ActiveModel) and records the predictions.
func NewStore(t *testing.T, db *sql.DB, model metalabel.Model) *metalabel.Store {
	t.Helper()
	store := metalabel.NewStore(repo.NewRepository(db))
	params, err := model.ParamsJSON()
	if err != nil {
		t.Fatalf("ParamsJSON: %v", err)
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.SaveModel(context.Background(), repo.Model{
		Version: model.Version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: repo.ModelActive, TrainedAt: at, TrainStart: at, TrainEnd: at, OOSStart: at, OOSEnd: at.Add(time.Hour),
		FeatureNames: model.FeatureNames, ParamsJSON: params,
	}); err != nil {
		t.Fatalf("SaveModel: %v", err)
	}
	return store
}

func ptr[T any](v T) *T { return &v }

// Feature is the snapshot feature set the model inputs come from (WithInputs'
// values): positive order flow and a 1-minute return.
func Feature() domain.Feature {
	return domain.Feature{
		Return1m:          ptr(0.001),
		OrderFlowFeatures: domain.OrderFlowFeatures{OFIShort: ptr(1.0), OFIMid: ptr(1.0), OFILong: ptr(1.0), MicropriceDevBps: ptr(0.5)},
	}
}

// WithInputs fills the meta-labeling model's inputs of in: Feature's order flow
// and return, and a neutral TDnet score.
func WithInputs(in policy.Input) policy.Input {
	f := Feature()
	in.OrderFlow, in.Return1m, in.TDnetScore = f.OrderFlowFeatures, f.Return1m, ptr(0.0)
	return in
}

// TDnet is a policy.TDnetScorer returning its value for every symbol.
type TDnet float64

// TDnetScore implements policy.TDnetScorer.
func (f TDnet) TDnetScore(string, time.Time) float64 { return float64(f) }

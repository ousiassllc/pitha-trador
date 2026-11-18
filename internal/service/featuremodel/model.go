// Package featuremodel trains and records the research-only feature model of
// issue #1092: a ridge regression on numeric market_snapshots features only (no AI,
// no Jev output) that predicts, for a trader decision's bar, the net bps a LONG and a
// SHORT held for 30 minutes would have made. It is trained walk-forward, nightly,
// and its predictions are recorded next to Jev's decisions so the candidate ledger
// can compare the two on the same rows.
//
// Research only: nothing it writes is a trading input (Policy, Risk, orders, the
// Paper guard, Live, the Jev prompts, RAG, the meta-model and self-improvement never
// read it; internal/repository/featuremodel/isolation_test.go pins that). It writes
// only research_model_runs and research_model_predictions. See
// docs/architecture/research-tools/research-model.md.
package featuremodel

import (
	"encoding/json"
	"fmt"
	"math"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// Production values of the model's hyperparameters. They are constants, not settings:
// a change is a new model version (Algorithm / the version prefix) and is recorded in
// every run's hyperparams_json. Params carries them so tests can use small fixtures.
const (
	// Algorithm is research_model_runs.algorithm.
	Algorithm = "ridge"
	// Horizon is the label horizon the model learns (research_horizon_labels.horizon).
	Horizon = "30m"
	// VersionPrefix names the method: rm-ridge1-30m-<predict day>.
	VersionPrefix = "rm-ridge1-" + Horizon + "-"

	// RidgeLambda is the L2 penalty on the standardized coefficients (not on the intercept).
	RidgeLambda = 1000.0
	// SampleBuckets uses the rows with sample_bucket < SampleBuckets (a deterministic
	// 10 % sample of the snapshots; the label table's bucket is a hash of snapshot_id).
	SampleBuckets = 10
	// MinTrainRows is the fewest rows each side's model is fitted on.
	MinTrainRows = 2000
	// MinTrainDays is the fewest complete label days the training window must span.
	MinTrainDays = 5
	// MaxTrainDays caps the expanding window to the most recent complete label days.
	MaxTrainDays = 60

	// minStd is the standard deviation below which a feature counts as constant
	// (its std is recorded as 1, so its standardized value is 0 and its coefficient 0).
	minStd = 1e-12
)

// Params are the hyperparameters of a training. DefaultParams is the production set;
// only tests use another (small fixtures).
type Params struct {
	RidgeLambda   float64
	SampleBuckets int
	MinTrainRows  int
	MinTrainDays  int
	MaxTrainDays  int
}

// DefaultParams returns the production hyperparameters.
func DefaultParams() Params {
	return Params{RidgeLambda: RidgeLambda, SampleBuckets: SampleBuckets, MinTrainRows: MinTrainRows, MinTrainDays: MinTrainDays, MaxTrainDays: MaxTrainDays}
}

// Version is research_model_runs.version of the run that predicts the JST day.
func Version(predictDay string) string { return VersionPrefix + predictDay }

// Side is one regression (LONG net bps or SHORT net bps) on the standardized features.
type Side struct {
	// N is the number of training rows the side was fitted on.
	N         int                `json:"n"`
	Intercept float64            `json:"intercept"`
	Coef      map[string]float64 `json:"coef"`
}

// Model is the fitted pair of regressions with the standardization they share.
// Mean and Std are the training mean and standard deviation of each feature over the
// observed values; a missing value is imputed with the mean, i.e. standardizes to 0.
type Model struct {
	Mean  map[string]float64 `json:"mean"`
	Std   map[string]float64 `json:"std"`
	Long  Side               `json:"long"`
	Short Side               `json:"short"`
}

// Predict returns the predicted LONG and SHORT net bps of a feature vector aligned
// with repo.Features (NaN: missing).
func (m Model) Predict(x []float64) (long, short float64) {
	long, short = m.Long.Intercept, m.Short.Intercept
	for i, name := range repo.Features {
		z := 0.0
		if !math.IsNaN(x[i]) {
			z = (x[i] - m.Mean[name]) / m.Std[name]
		}
		long = float64(m.Long.Coef[name]*z) + long
		short = float64(m.Short.Coef[name]*z) + short
	}
	return long, short
}

// Direction is the model's pick for a pair of predictions: LONG when the long
// prediction is the larger and positive, SHORT likewise, otherwise NONE.
func Direction(long, short float64) string {
	switch {
	case long > short && long > 0:
		return "LONG"
	case short > long && short > 0:
		return "SHORT"
	}
	return "NONE"
}

// hyperparams is research_model_runs.hyperparams_json.
type hyperparams struct {
	LabelHorizon    string  `json:"label_horizon"`
	RidgeLambda     float64 `json:"ridge_lambda"`
	SampleBuckets   int     `json:"sample_buckets"`
	MinTrainRows    int     `json:"min_train_rows"`
	MinTrainDays    int     `json:"min_train_days"`
	MaxTrainDays    int     `json:"max_train_days"`
	Standardization string  `json:"standardization"`
	Imputation      string  `json:"imputation"`
	Solver          string  `json:"solver"`
	Purge           string  `json:"purge"`
}

func hyperparamsJSON(p Params) (string, error) {
	return marshal(hyperparams{
		LabelHorizon: Horizon, RidgeLambda: p.RidgeLambda, SampleBuckets: p.SampleBuckets,
		MinTrainRows: p.MinTrainRows, MinTrainDays: p.MinTrainDays, MaxTrainDays: p.MaxTrainDays,
		Standardization: "z-score by the training mean and population std (std=1 when the variance is 0)",
		Imputation:      "training mean",
		Solver:          "ridge closed form (streamed X'X and X'y, Cholesky), intercept not penalized",
		Purge:           "rows whose exit_at is not before the start of the predicted JST day are dropped",
	})
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("featuremodel: encode json: %w", err)
	}
	return string(b), nil
}

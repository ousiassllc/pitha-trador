package metalabel

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// AlgorithmLogisticRegression is the algorithm name stored in
// metalabel_models.algorithm for Model. LightGBM-class models are too heavy
// for Go, so the first secondary model is a logistic regression (issue #758).
const AlgorithmLogisticRegression = "logistic_regression"

// Model is the trained secondary model of the meta-labeling pipeline: a
// logistic regression over Features.Vector, a Calibrator, and the payoff of
// a win and a loss in bps (both after trading costs, from net_return_bps).
// The primary signal (Jev's direction) is not part of it.
type Model struct {
	// ID is metalabel_models.id of the stored row the model was parsed from
	// (ParseModel); 0 for a model that is not stored.
	ID int64
	// Version is metalabel_models.version.
	Version string
	// FeatureNames is the vector layout the weights were learned for.
	FeatureNames []string
	LR           LogisticRegression
	Calibrator   Calibrator
	// WinBps is the mean net return of winning samples (> 0) and LossBps the
	// mean net loss (>= 0, positive number) of losing ones. They turn a
	// calibrated probability p into the expected value p*WinBps - (1-p)*LossBps.
	WinBps, LossBps float64
}

// modelParams is the JSON of metalabel_models.params_json for Model.
type modelParams struct {
	LogisticRegression LogisticRegression `json:"logistic_regression"`
	Calibrator         Calibrator         `json:"calibrator"`
	WinBps             float64            `json:"win_bps"`
	LossBps            float64            `json:"loss_bps"`
}

// ParamsJSON encodes the parameters for metalabel_models.params_json.
func (m Model) ParamsJSON() (string, error) {
	b, err := json.Marshal(modelParams{LogisticRegression: m.LR, Calibrator: m.Calibrator, WinBps: m.WinBps, LossBps: m.LossBps})
	if err != nil {
		return "", fmt.Errorf("metalabel: encode model %q params: %w", m.Version, err)
	}
	return string(b), nil
}

// ParseModel rebuilds the Model from a stored metalabel_models row. It fails
// (the caller must skip the signal) for a non-active row, another algorithm,
// a feature layout other than FeatureNames, or parameters that are missing,
// non-finite or inconsistent.
func ParseModel(m repo.Model) (Model, error) {
	if m.Status != repo.ModelActive {
		return Model{}, fmt.Errorf("metalabel: model %q is %q, not active", m.Version, m.Status)
	}
	if m.Algorithm != AlgorithmLogisticRegression {
		return Model{}, fmt.Errorf("metalabel: model %q has unsupported algorithm %q", m.Version, m.Algorithm)
	}
	if !slices.Equal(m.FeatureNames, featureNames) {
		return Model{}, fmt.Errorf("metalabel: model %q was trained on features %v, this build uses %v", m.Version, m.FeatureNames, featureNames)
	}
	var p modelParams
	if err := json.Unmarshal([]byte(m.ParamsJSON), &p); err != nil {
		return Model{}, fmt.Errorf("metalabel: decode model %q params: %w", m.Version, err)
	}
	model := Model{ID: m.ID, Version: m.Version, FeatureNames: slices.Clone(m.FeatureNames), LR: p.LogisticRegression, Calibrator: p.Calibrator, WinBps: p.WinBps, LossBps: p.LossBps}
	if err := model.validate(); err != nil {
		return Model{}, fmt.Errorf("metalabel: model %q: %w", m.Version, err)
	}
	return model, nil
}

func (m Model) validate() error {
	if err := m.LR.validate(); err != nil {
		return err
	}
	if len(m.LR.Weights) != len(featureNames) {
		return fmt.Errorf("model has %d weights for %d features", len(m.LR.Weights), len(featureNames))
	}
	if err := m.Calibrator.validate(); err != nil {
		return err
	}
	if !finite(m.WinBps) || !finite(m.LossBps) || m.WinBps <= 0 || m.LossBps < 0 {
		return errors.New("win_bps must be positive and loss_bps non-negative")
	}
	return nil
}

// Prediction is Model.Predict's result.
type Prediction struct {
	// Raw is the model's win probability before calibration.
	Raw float64
	// Calibrated is the win probability after calibration.
	Calibrated float64
	// ExpectedValueBps is the cost-adjusted expected value computed from
	// Calibrated.
	ExpectedValueBps float64
}

// Predict scores a Features.Vector. The expected value is always derived from
// the calibrated probability, never the raw one.
func (m Model) Predict(vector []float64) (Prediction, error) {
	raw, err := m.LR.Probability(vector)
	if err != nil {
		return Prediction{}, err
	}
	calibrated, err := m.Calibrator.Calibrate(raw)
	if err != nil {
		return Prediction{}, err
	}
	return Prediction{Raw: raw, Calibrated: calibrated, ExpectedValueBps: m.ExpectedValueBps(calibrated)}, nil
}

// ExpectedValueBps is the expected net return of taking a signal that wins
// with probability p: p*WinBps - (1-p)*LossBps.
func (m Model) ExpectedValueBps(p float64) float64 {
	return p*m.WinBps - (1-p)*m.LossBps
}

// Sample is one labeled training row: the oriented feature vector the signal
// had, whether it won (net_return_bps > 0) and its net return in bps.
type Sample struct {
	Vector       []float64
	NetReturnBps float64
}

// Win reports whether the sample's net return is positive.
func (s Sample) Win() bool { return s.NetReturnBps > 0 }

// TrainOptions configures Train.
type TrainOptions struct {
	// Version is the model version (metalabel_models.version), required.
	Version string
	// Calibration is CalibrationPlatt or CalibrationIsotonic.
	Calibration string
	// LR configures the logistic regression.
	LR TrainConfig
}

// Train fits the logistic regression on fit and the calibrator on calibrate,
// then takes the win/loss payoff from fit. The two sets MUST be disjoint,
// ideally fit older than calibrate (calibrating on the data the weights were
// fitted to would report probabilities that are too confident); choosing the
// split and the OOS evaluation is the caller's job. It fails when either set
// is unusable (empty, ragged, single-class).
func Train(fit, calibrate []Sample, opts TrainOptions) (Model, error) {
	if opts.Version == "" {
		return Model{}, errors.New("metalabel: model version is empty")
	}
	x, y := splitSamples(fit)
	lr, err := TrainLogisticRegression(x, y, opts.LR)
	if err != nil {
		return Model{}, err
	}
	if len(lr.Weights) != len(featureNames) {
		return Model{}, fmt.Errorf("%w: samples have %d features, want %d", ErrInvalidTrainingData, len(lr.Weights), len(featureNames))
	}

	cx, cy := splitSamples(calibrate)
	raw := make([]float64, len(cx))
	for i, row := range cx {
		if raw[i], err = lr.Probability(row); err != nil {
			return Model{}, fmt.Errorf("metalabel: score calibration sample %d: %w", i, err)
		}
	}
	cal, err := FitCalibrator(opts.Calibration, raw, cy)
	if err != nil {
		return Model{}, err
	}

	var winSum, lossSum float64
	var wins, losses int
	for _, s := range fit {
		if s.Win() {
			winSum += s.NetReturnBps
			wins++
		} else {
			lossSum -= s.NetReturnBps
			losses++
		}
	}
	m := Model{
		Version: opts.Version, FeatureNames: slices.Clone(featureNames), LR: lr, Calibrator: cal,
		WinBps: winSum / float64(wins), LossBps: lossSum / float64(losses),
	}
	if err := m.validate(); err != nil {
		return Model{}, fmt.Errorf("metalabel: trained model is unusable: %w", err)
	}
	return m, nil
}

func splitSamples(samples []Sample) (x [][]float64, y []bool) {
	x = make([][]float64, len(samples))
	y = make([]bool, len(samples))
	for i, s := range samples {
		x[i], y[i] = s.Vector, s.Win()
	}
	return x, y
}

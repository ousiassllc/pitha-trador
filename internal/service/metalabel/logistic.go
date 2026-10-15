package metalabel

import (
	"errors"
	"fmt"
	"math"
)

// TrainConfig controls TrainLogisticRegression's full-batch gradient descent
// on standardized features.
type TrainConfig struct {
	// L2 is the ridge penalty on the weights (not the bias), >= 0.
	L2 float64
	// Iterations is the number of gradient steps, > 0.
	Iterations int
	// LearningRate is the step size, > 0.
	LearningRate float64
}

// DefaultTrainConfig returns a configuration that converges on the standardized
// 8-feature input within a few thousand samples.
func DefaultTrainConfig() TrainConfig {
	return TrainConfig{L2: 0.01, Iterations: 2000, LearningRate: 0.5}
}

// LogisticRegression is a trained logistic regression over standardized
// features: P(win) = sigmoid(Bias + sum_i Weights[i] * (x[i]-Mean[i]) / Std[i]).
// It is a plain value: training is deterministic and nothing here is stored
// or scheduled (that is the caller's job).
type LogisticRegression struct {
	Mean    []float64 `json:"mean"`
	Std     []float64 `json:"std"`
	Weights []float64 `json:"weights"`
	Bias    float64   `json:"bias"`
}

// ErrInvalidTrainingData is wrapped by TrainLogisticRegression's error for
// data a model cannot be learned from.
var ErrInvalidTrainingData = errors.New("metalabel: invalid training data")

// TrainLogisticRegression fits a logistic regression on x (one feature vector
// per row, all of the same non-zero length) and y (true = the primary signal
// won, i.e. net_return_bps > 0). It fails on empty or ragged data, a
// non-finite value, or labels of a single class (nothing to separate), never
// returning a half-fitted model.
func TrainLogisticRegression(x [][]float64, y []bool, cfg TrainConfig) (LogisticRegression, error) {
	if cfg.Iterations <= 0 || cfg.LearningRate <= 0 || cfg.L2 < 0 || math.IsNaN(cfg.L2) {
		return LogisticRegression{}, fmt.Errorf("%w: bad train config %+v", ErrInvalidTrainingData, cfg)
	}
	n := len(x)
	if n == 0 || n != len(y) {
		return LogisticRegression{}, fmt.Errorf("%w: %d rows, %d labels", ErrInvalidTrainingData, n, len(y))
	}
	dim := len(x[0])
	if dim == 0 {
		return LogisticRegression{}, fmt.Errorf("%w: rows have no features", ErrInvalidTrainingData)
	}
	wins := 0
	for i, row := range x {
		if len(row) != dim {
			return LogisticRegression{}, fmt.Errorf("%w: row %d has %d features, want %d", ErrInvalidTrainingData, i, len(row), dim)
		}
		for j, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return LogisticRegression{}, fmt.Errorf("%w: row %d feature %d is not finite", ErrInvalidTrainingData, i, j)
			}
		}
		if y[i] {
			wins++
		}
	}
	if wins == 0 || wins == n {
		return LogisticRegression{}, fmt.Errorf("%w: all %d labels are the same class", ErrInvalidTrainingData, n)
	}

	lr := LogisticRegression{Mean: make([]float64, dim), Std: make([]float64, dim), Weights: make([]float64, dim)}
	for _, row := range x {
		for j, v := range row {
			lr.Mean[j] += v
		}
	}
	for j := range lr.Mean {
		lr.Mean[j] /= float64(n)
	}
	for _, row := range x {
		for j, v := range row {
			d := v - lr.Mean[j]
			lr.Std[j] += d * d
		}
	}
	for j := range lr.Std {
		lr.Std[j] = math.Sqrt(lr.Std[j] / float64(n))
		if lr.Std[j] == 0 {
			lr.Std[j] = 1 // a constant feature carries no signal; keep it at 0 after centering
		}
	}
	z := make([][]float64, n)
	for i, row := range x {
		z[i] = lr.standardize(row)
	}

	// Start from the base rate so the first steps already match the prior.
	rate := float64(wins) / float64(n)
	lr.Bias = math.Log(rate / (1 - rate))
	grad := make([]float64, dim)
	for range cfg.Iterations {
		clear(grad)
		var gradBias float64
		for i, row := range z {
			err := sigmoid(lr.Bias+dot(lr.Weights, row)) - label(y[i])
			gradBias += err
			for j, v := range row {
				grad[j] += err * v
			}
		}
		lr.Bias -= cfg.LearningRate * gradBias / float64(n)
		for j := range lr.Weights {
			lr.Weights[j] -= cfg.LearningRate * (grad[j]/float64(n) + cfg.L2*lr.Weights[j])
		}
	}
	return lr, nil
}

// Probability returns P(win) for x, the raw (uncalibrated) score. It fails on
// a vector of the wrong length or with a non-finite value.
func (lr LogisticRegression) Probability(x []float64) (float64, error) {
	if len(x) != len(lr.Weights) {
		return 0, fmt.Errorf("metalabel: feature vector has %d values, model expects %d", len(x), len(lr.Weights))
	}
	for i, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("%w: value %d is not finite", ErrMissingFeature, i)
		}
	}
	return sigmoid(lr.Bias + dot(lr.Weights, lr.standardize(x))), nil
}

func (lr LogisticRegression) standardize(x []float64) []float64 {
	z := make([]float64, len(x))
	for j, v := range x {
		z[j] = (v - lr.Mean[j]) / lr.Std[j]
	}
	return z
}

func (lr LogisticRegression) validate() error {
	dim := len(lr.Weights)
	if dim == 0 || len(lr.Mean) != dim || len(lr.Std) != dim {
		return fmt.Errorf("metalabel: logistic regression has inconsistent dimensions (weights %d, mean %d, std %d)", dim, len(lr.Mean), len(lr.Std))
	}
	if !finite(lr.Bias) {
		return errors.New("metalabel: logistic regression bias is not finite")
	}
	for j := range dim {
		if !finite(lr.Weights[j]) || !finite(lr.Mean[j]) || !finite(lr.Std[j]) || lr.Std[j] <= 0 {
			return fmt.Errorf("metalabel: logistic regression parameter %d is not finite or std is not positive", j)
		}
	}
	return nil
}

func sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func label(win bool) float64 {
	if win {
		return 1
	}
	return 0
}

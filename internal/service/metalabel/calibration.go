package metalabel

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

// Calibration methods.
const (
	// CalibrationPlatt maps the logit of the raw probability through a fitted
	// sigmoid (Platt scaling): two parameters, stable on small samples.
	CalibrationPlatt = "platt"
	// CalibrationIsotonic fits a monotone step function (pool-adjacent-
	// violators): no shape assumption, needs more samples.
	CalibrationIsotonic = "isotonic"
)

// Calibrator turns the model's raw win probability into a calibrated one, the
// only probability expected value is computed from. It is a plain value that
// round-trips through JSON (metalabel_models.params_json).
type Calibrator struct {
	Method string `json:"method"`
	// A and B are Platt's parameters: p = sigmoid(A*logit(raw) + B).
	A float64 `json:"a,omitempty"`
	B float64 `json:"b,omitempty"`
	// X and Y are isotonic's knots: raw probabilities ascending and the
	// calibrated value (non-decreasing) at each; between knots the value is
	// interpolated linearly, outside it is held at the end knot.
	X []float64 `json:"x,omitempty"`
	Y []float64 `json:"y,omitempty"`
}

// ErrInvalidCalibration is wrapped by the errors FitCalibrator and
// Calibrator.validate return.
var ErrInvalidCalibration = errors.New("metalabel: invalid calibration")

// FitCalibrator fits method to raw probabilities (each in [0, 1]) and the
// outcomes (true = win). It fails on empty or mismatched input, a probability
// outside [0, 1], a single-class label set, or an unknown method.
func FitCalibrator(method string, raw []float64, y []bool) (Calibrator, error) {
	if len(raw) == 0 || len(raw) != len(y) {
		return Calibrator{}, fmt.Errorf("%w: %d probabilities, %d labels", ErrInvalidCalibration, len(raw), len(y))
	}
	wins := 0
	for i, p := range raw {
		if !(p >= 0 && p <= 1) {
			return Calibrator{}, fmt.Errorf("%w: probability %d (%v) is outside [0, 1]", ErrInvalidCalibration, i, p)
		}
		if y[i] {
			wins++
		}
	}
	if wins == 0 || wins == len(y) {
		return Calibrator{}, fmt.Errorf("%w: all %d labels are the same class", ErrInvalidCalibration, len(y))
	}
	switch method {
	case CalibrationPlatt:
		return fitPlatt(raw, y, wins), nil
	case CalibrationIsotonic:
		return fitIsotonic(raw, y), nil
	}
	return Calibrator{}, fmt.Errorf("%w: unknown method %q", ErrInvalidCalibration, method)
}

// Calibrate returns the calibrated probability for raw, in [0, 1].
func (c Calibrator) Calibrate(raw float64) (float64, error) {
	if !(raw >= 0 && raw <= 1) {
		return 0, fmt.Errorf("%w: probability %v is outside [0, 1]", ErrInvalidCalibration, raw)
	}
	switch c.Method {
	case CalibrationPlatt:
		return sigmoid(c.A*logit(raw) + c.B), nil
	case CalibrationIsotonic:
		return c.interpolate(raw), nil
	}
	return 0, fmt.Errorf("%w: unknown method %q", ErrInvalidCalibration, c.Method)
}

func (c Calibrator) validate() error {
	switch c.Method {
	case CalibrationPlatt:
		if !finite(c.A) || !finite(c.B) {
			return fmt.Errorf("%w: platt parameters are not finite", ErrInvalidCalibration)
		}
	case CalibrationIsotonic:
		if len(c.X) == 0 || len(c.X) != len(c.Y) {
			return fmt.Errorf("%w: isotonic has %d knots for %d values", ErrInvalidCalibration, len(c.X), len(c.Y))
		}
		for i := range c.X {
			if !(c.X[i] >= 0 && c.X[i] <= 1 && c.Y[i] >= 0 && c.Y[i] <= 1) {
				return fmt.Errorf("%w: isotonic knot %d is outside [0, 1]", ErrInvalidCalibration, i)
			}
			if i > 0 && (c.X[i] < c.X[i-1] || c.Y[i] < c.Y[i-1]) {
				return fmt.Errorf("%w: isotonic knot %d is not monotone", ErrInvalidCalibration, i)
			}
		}
	default:
		return fmt.Errorf("%w: unknown method %q", ErrInvalidCalibration, c.Method)
	}
	return nil
}

func (c Calibrator) interpolate(raw float64) float64 {
	n := len(c.X)
	if raw <= c.X[0] {
		return c.Y[0]
	}
	if raw >= c.X[n-1] {
		return c.Y[n-1]
	}
	i := sort.SearchFloat64s(c.X, raw) // first knot >= raw; 0 < i < n here
	if c.X[i] == raw {
		return c.Y[i]
	}
	frac := (raw - c.X[i-1]) / (c.X[i] - c.X[i-1])
	return c.Y[i-1] + frac*(c.Y[i]-c.Y[i-1])
}

// probEps keeps logit finite for raw probabilities of exactly 0 or 1.
const probEps = 1e-6

func logit(p float64) float64 {
	p = math.Min(math.Max(p, probEps), 1-probEps)
	return math.Log(p / (1 - p))
}

// fitPlatt minimizes the cross-entropy of sigmoid(A*s+B) against Platt's
// smoothed targets (which keep A, B finite on separable data) by Newton's
// method with step halving.
func fitPlatt(raw []float64, y []bool, wins int) Calibrator {
	n := len(raw)
	tPos := (float64(wins) + 1) / (float64(wins) + 2)
	tNeg := 1 / (float64(n-wins) + 2)
	s := make([]float64, n)
	t := make([]float64, n)
	for i, p := range raw {
		s[i] = logit(p)
		t[i] = tNeg
		if y[i] {
			t[i] = tPos
		}
	}
	loss := func(a, b float64) float64 {
		var l float64
		for i := range s {
			z := a*s[i] + b
			// log(1+e^z) - t*z, stable for large |z|.
			l += math.Max(z, 0) + math.Log1p(math.Exp(-math.Abs(z))) - t[i]*z
		}
		return l
	}

	// Start from the prior: the identity map on the logit scale, with the bias
	// that matches the base rate.
	a, b := 1.0, 0.0
	cur := loss(a, b)
	for range 100 {
		var ga, gb, haa, hab, hbb float64
		for i := range s {
			p := sigmoid(a*s[i] + b)
			d := p - t[i]
			w := p * (1 - p)
			ga += d * s[i]
			gb += d
			haa += w * s[i] * s[i]
			hab += w * s[i]
			hbb += w
		}
		// Ridge keeps the Hessian invertible when s is (near) constant.
		haa += 1e-9
		hbb += 1e-9
		det := haa*hbb - hab*hab
		if det <= 0 || math.IsNaN(det) {
			break
		}
		da := (hbb*ga - hab*gb) / det
		db := (haa*gb - hab*ga) / det
		step := 1.0
		improved := false
		for range 30 {
			na, nb := a-step*da, b-step*db
			if l := loss(na, nb); l <= cur {
				a, b, cur = na, nb, l
				improved = true
				break
			}
			step /= 2
		}
		if !improved || math.Abs(step*da) < 1e-10 && math.Abs(step*db) < 1e-10 {
			break
		}
	}
	return Calibrator{Method: CalibrationPlatt, A: a, B: b}
}

// fitIsotonic runs pool-adjacent-violators on the labels ordered by raw
// probability and keeps one knot per pooled block, at the block's mean raw
// probability.
func fitIsotonic(raw []float64, y []bool) Calibrator {
	idx := make([]int, len(raw))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		switch {
		case raw[a] < raw[b]:
			return -1
		case raw[a] > raw[b]:
			return 1
		}
		return 0
	})

	type block struct{ sumX, sumY, n float64 }
	var blocks []block
	for _, i := range idx {
		blk := block{sumX: raw[i], sumY: label(y[i]), n: 1}
		for len(blocks) > 0 {
			prev := blocks[len(blocks)-1]
			if prev.sumY/prev.n <= blk.sumY/blk.n {
				break
			}
			blk = block{sumX: prev.sumX + blk.sumX, sumY: prev.sumY + blk.sumY, n: prev.n + blk.n}
			blocks = blocks[:len(blocks)-1]
		}
		blocks = append(blocks, blk)
	}

	c := Calibrator{Method: CalibrationIsotonic, X: make([]float64, 0, len(blocks)), Y: make([]float64, 0, len(blocks))}
	for _, blk := range blocks {
		x := blk.sumX / blk.n
		// Equal mean raw probabilities would break the strictly ascending
		// knots interpolate relies on; the later block is monotone-above, so
		// replace the earlier knot's value.
		if k := len(c.X); k > 0 && c.X[k-1] >= x {
			c.Y[k-1] = blk.sumY / blk.n
			continue
		}
		c.X = append(c.X, x)
		c.Y = append(c.Y, blk.sumY/blk.n)
	}
	return c
}

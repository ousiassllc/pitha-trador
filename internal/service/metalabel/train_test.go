package metalabel_test

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

func syntheticSamples(rng *rand.Rand, n int) []metalabel.Sample {
	out := make([]metalabel.Sample, n)
	for i := range out {
		v := make([]float64, len(metalabel.FeatureNames()))
		for j := range v {
			v[j] = rng.NormFloat64()
		}
		pWin := 1 / (1 + math.Exp(-2*v[0]))
		net := -20.0
		if rng.Float64() < pWin {
			net = 30
		}
		out[i] = metalabel.Sample{Vector: v, NetReturnBps: net}
	}
	return out
}

func TestTrain_LearnsTheSignalAndCalibrates(t *testing.T) {
	for _, method := range []string{metalabel.CalibrationPlatt, metalabel.CalibrationIsotonic} {
		rng := rand.New(rand.NewPCG(1, 2))
		fit, calib := syntheticSamples(rng, 4000), syntheticSamples(rng, 4000)
		m, err := metalabel.Train(fit, calib, metalabel.TrainOptions{Version: "v1", Calibration: method, LR: metalabel.DefaultTrainConfig()})
		if err != nil {
			t.Fatalf("%s: Train: %v", method, err)
		}
		if m.WinBps != 30 || m.LossBps != 20 {
			t.Errorf("%s: payoff = %v/%v, want 30/20", method, m.WinBps, m.LossBps)
		}

		strong := make([]float64, len(m.FeatureNames))
		strong[0] = 2
		weak := make([]float64, len(m.FeatureNames))
		weak[0] = -2
		ps, err := m.Predict(strong)
		if err != nil {
			t.Fatal(err)
		}
		pw, err := m.Predict(weak)
		if err != nil {
			t.Fatal(err)
		}
		if ps.Calibrated <= pw.Calibrated+0.3 {
			t.Errorf("%s: strong %.3f vs weak %.3f: the signal feature was not learned", method, ps.Calibrated, pw.Calibrated)
		}
		// True win rate at x0=2 is sigmoid(4)=0.982, at -2 it is 0.018.
		if math.Abs(ps.Calibrated-0.98) > 0.08 || math.Abs(pw.Calibrated-0.02) > 0.08 {
			t.Errorf("%s: calibrated probabilities %.3f/%.3f far from 0.98/0.02", method, ps.Calibrated, pw.Calibrated)
		}
		if want := ps.Calibrated*30 - (1-ps.Calibrated)*20; math.Abs(ps.ExpectedValueBps-want) > 1e-9 {
			t.Errorf("%s: EV = %v, want computed from the calibrated probability %v", method, ps.ExpectedValueBps, want)
		}
	}
}

func TestTrain_RejectsUnusableData(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	good := syntheticSamples(rng, 200)
	opts := metalabel.TrainOptions{Version: "v1", Calibration: metalabel.CalibrationPlatt, LR: metalabel.DefaultTrainConfig()}

	allLoss := make([]metalabel.Sample, 50)
	for i := range allLoss {
		allLoss[i] = metalabel.Sample{Vector: good[i].Vector, NetReturnBps: -5}
	}
	ragged := append([]metalabel.Sample{{Vector: []float64{1}, NetReturnBps: 1}}, good...)
	nan := append([]metalabel.Sample{{Vector: append([]float64{math.NaN()}, good[0].Vector[1:]...), NetReturnBps: 1}}, good...)
	shortVec := []metalabel.Sample{{Vector: []float64{1, 2}, NetReturnBps: 1}, {Vector: []float64{2, 1}, NetReturnBps: -1}}

	cases := map[string]func() error{
		"empty fit":           func() error { _, err := metalabel.Train(nil, good, opts); return err },
		"empty calibration":   func() error { _, err := metalabel.Train(good, nil, opts); return err },
		"single-class fit":    func() error { _, err := metalabel.Train(allLoss, good, opts); return err },
		"single-class calib":  func() error { _, err := metalabel.Train(good, allLoss, opts); return err },
		"ragged":              func() error { _, err := metalabel.Train(ragged, good, opts); return err },
		"NaN feature":         func() error { _, err := metalabel.Train(nan, good, opts); return err },
		"wrong feature count": func() error { _, err := metalabel.Train(shortVec, shortVec, opts); return err },
		"no version": func() error {
			_, err := metalabel.Train(good, good, metalabel.TrainOptions{Calibration: "platt", LR: opts.LR})
			return err
		},
		"unknown calibration": func() error {
			_, err := metalabel.Train(good, good, metalabel.TrainOptions{Version: "v", Calibration: "x", LR: opts.LR})
			return err
		},
		"bad config": func() error {
			_, err := metalabel.Train(good, good, metalabel.TrainOptions{Version: "v", Calibration: "platt"})
			return err
		},
	}
	for name, f := range cases {
		if f() == nil {
			t.Errorf("%s: Train succeeded", name)
		}
	}
}

func TestFitCalibrator_IsotonicIsMonotoneAndPlattFixesOverconfidence(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	n := 3000
	raw := make([]float64, n)
	y := make([]bool, n)
	for i := range raw {
		raw[i] = rng.Float64()
		// The model is overconfident: the real win rate is squeezed towards 0.5.
		y[i] = rng.Float64() < 0.5+(raw[i]-0.5)*0.4
	}
	iso, err := metalabel.FitCalibrator(metalabel.CalibrationIsotonic, raw, y)
	if err != nil {
		t.Fatal(err)
	}
	prev := -1.0
	for p := 0.0; p <= 1; p += 0.01 {
		c, err := iso.Calibrate(p)
		if err != nil || c < prev-1e-12 || c < 0 || c > 1 {
			t.Fatalf("isotonic(%v) = %v (prev %v), %v", p, c, prev, err)
		}
		prev = c
	}
	platt, err := metalabel.FitCalibrator(metalabel.CalibrationPlatt, raw, y)
	if err != nil {
		t.Fatal(err)
	}
	for _, cal := range []metalabel.Calibrator{iso, platt} {
		hi, _ := cal.Calibrate(0.95)
		lo, _ := cal.Calibrate(0.05)
		if math.Abs(hi-0.68) > 0.07 || math.Abs(lo-0.32) > 0.07 {
			t.Errorf("%s: calibrate(0.95)=%.3f calibrate(0.05)=%.3f, want about 0.68/0.32", cal.Method, hi, lo)
		}
	}
}

func TestFitCalibrator_Rejects(t *testing.T) {
	cases := map[string]struct {
		method string
		raw    []float64
		y      []bool
	}{
		"empty":        {metalabel.CalibrationPlatt, nil, nil},
		"mismatched":   {metalabel.CalibrationPlatt, []float64{0.5}, []bool{true, false}},
		"out of range": {metalabel.CalibrationIsotonic, []float64{1.5, 0.2}, []bool{true, false}},
		"NaN":          {metalabel.CalibrationIsotonic, []float64{math.NaN(), 0.2}, []bool{true, false}},
		"single class": {metalabel.CalibrationPlatt, []float64{0.4, 0.6}, []bool{true, true}},
		"unknown":      {"beta", []float64{0.4, 0.6}, []bool{true, false}},
	}
	for name, tc := range cases {
		if _, err := metalabel.FitCalibrator(tc.method, tc.raw, tc.y); !errors.Is(err, metalabel.ErrInvalidCalibration) {
			t.Errorf("%s: error = %v, want ErrInvalidCalibration", name, err)
		}
	}
	if _, err := (metalabel.Calibrator{Method: metalabel.CalibrationPlatt}).Calibrate(1.2); err == nil {
		t.Error("Calibrate accepted a probability above 1")
	}
}

func trainedModel(t *testing.T) metalabel.Model {
	t.Helper()
	rng := rand.New(rand.NewPCG(7, 8))
	m, err := metalabel.Train(syntheticSamples(rng, 1000), syntheticSamples(rng, 1000),
		metalabel.TrainOptions{Version: "v1", Calibration: metalabel.CalibrationIsotonic, LR: metalabel.DefaultTrainConfig()})
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	return m
}

func storedRow(t *testing.T, m metalabel.Model) repo.Model {
	t.Helper()
	params, err := m.ParamsJSON()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	return repo.Model{
		Version: m.Version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: 2, Status: repo.ModelActive,
		TrainedAt: now, TrainStart: now, TrainEnd: now, OOSStart: now, OOSEnd: now,
		FeatureNames: metalabel.FeatureNames(), ParamsJSON: params,
	}
}

func TestParseModel_RoundTripsAndRejectsUnsafeRows(t *testing.T) {
	m := trainedModel(t)
	row := storedRow(t, m)

	parsed, err := metalabel.ParseModel(row)
	if err != nil {
		t.Fatalf("ParseModel: %v", err)
	}
	vec := make([]float64, len(m.FeatureNames))
	vec[0] = 1
	a, _ := m.Predict(vec)
	b, err := parsed.Predict(vec)
	if err != nil || a != b {
		t.Fatalf("round trip changed the prediction: %+v vs %+v (%v)", a, b, err)
	}

	mutate := map[string]func(*repo.Model){
		"rejected":        func(r *repo.Model) { r.Status = repo.ModelRejected },
		"other algorithm": func(r *repo.Model) { r.Algorithm = "lightgbm" },
		"other features":  func(r *repo.Model) { r.FeatureNames = r.FeatureNames[:len(r.FeatureNames)-1] },
		"reordered": func(r *repo.Model) {
			r.FeatureNames = append([]string{r.FeatureNames[1], r.FeatureNames[0]}, r.FeatureNames[2:]...)
		},
		"not json":     func(r *repo.Model) { r.ParamsJSON = "{" },
		"empty params": func(r *repo.Model) { r.ParamsJSON = "{}" },
		"zero win": func(r *repo.Model) {
			r.ParamsJSON = `{"logistic_regression":{"mean":[0,0,0,0,0,0,0,0],"std":[1,1,1,1,1,1,1,1],"weights":[0,0,0,0,0,0,0,0],"bias":0},"calibrator":{"method":"platt"},"win_bps":0,"loss_bps":1}`
		},
	}
	for name, f := range mutate {
		bad := row
		bad.FeatureNames = append([]string(nil), row.FeatureNames...)
		f(&bad)
		if _, err := metalabel.ParseModel(bad); err == nil {
			t.Errorf("%s: ParseModel succeeded", name)
		}
	}
}

// constantModel always predicts the calibrated probability p: zero weights give

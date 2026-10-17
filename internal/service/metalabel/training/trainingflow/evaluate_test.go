package trainingflow_test

import (
	"math"
	"math/rand/v2"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

func TestAcceptanceConfig_Judge(t *testing.T) {
	cfg := training.DefaultAcceptanceConfig()
	f := func(v float64) *float64 { return &v }
	iv := func(low, high float64) *repo.Interval { return &repo.Interval{Low: low, High: high} }
	good := training.Evaluation{Samples: cfg.MinOOSSamples, AUC: f(0.56), Takes: cfg.MinTakes, EVBps: f(5)}
	goodIv := training.Intervals{Days: cfg.MinOOSDays, EV: iv(0.5, 9), AUC: iv(0.51, 0.6)}
	if status, reason := cfg.Judge(good, goodIv); status != repo.ModelActive || reason != "" {
		t.Fatalf("good evaluation = %q (%s), want active", status, reason)
	}
	for name, tc := range map[string]struct {
		mutE  func(*training.Evaluation)
		mutIv func(*training.Intervals)
		want  string
	}{
		"few samples":        {func(e *training.Evaluation) { e.Samples-- }, nil, training.RejectOOSSamples},
		"few days":           {nil, func(i *training.Intervals) { i.Days-- }, training.RejectOOSDays},
		"few takes":          {func(e *training.Evaluation) { e.Takes-- }, nil, training.RejectTakes},
		"undefined EV CI":    {nil, func(i *training.Intervals) { i.EV = nil }, training.RejectEV},
		"EV CI includes 0":   {nil, func(i *training.Intervals) { i.EV = iv(0, 9) }, training.RejectEV},
		"EV CI below 0":      {nil, func(i *training.Intervals) { i.EV = iv(-4, -1) }, training.RejectEV},
		"undefined AUC CI":   {nil, func(i *training.Intervals) { i.AUC = nil }, training.RejectAUC},
		"AUC CI includes .5": {nil, func(i *training.Intervals) { i.AUC = iv(0.5, 0.6) }, training.RejectAUC},
	} {
		e, ivs := good, goodIv
		if tc.mutE != nil {
			tc.mutE(&e)
		}
		if tc.mutIv != nil {
			tc.mutIv(&ivs)
		}
		if status, reason := cfg.Judge(e, ivs); status != repo.ModelRejected || reason != tc.want {
			t.Errorf("%s: Judge = %q (%s), want rejected (%s)", name, status, reason, tc.want)
		}
	}
}

func TestEvaluate_ReportsAUCEVAndReliabilityOnTheOOSRows(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	fit, calib := syntheticSamples(rng, 4000), syntheticSamples(rng, 4000)
	m, err := metalabel.Train(fit, calib, metalabel.TrainOptions{Version: "v", Calibration: metalabel.CalibrationPlatt, LR: metalabel.DefaultTrainConfig()})
	if err != nil {
		t.Fatal(err)
	}
	var rows []training.Row
	for _, s := range syntheticSamples(rng, 1000) {
		f := make([]*float64, len(s.Vector))
		for i := range s.Vector {
			f[i] = &s.Vector[i]
		}
		rows = append(rows, training.Row{
			Direction: "LONG", Vector: s.Vector, NetReturnBps: s.NetReturnBps,
			Features: metalabel.Features{OFIShort: f[0], OFIMid: f[1], OFILong: f[2], MicropriceDevBps: f[3], SpreadBps: f[4], Return1m: f[5], JevScore: f[6], TDnetScore: f[7]},
		})
	}
	e := training.Evaluate(m, rows, metalabel.DefaultDecideConfig())
	if e.Samples != 1000 {
		t.Fatalf("Samples = %d, want 1000", e.Samples)
	}
	if e.AUC == nil || *e.AUC < 0.7 {
		t.Errorf("AUC = %v, want a model that learned the signal", e.AUC)
	}
	if e.Takes == 0 || e.Takes > e.Samples || e.EVBps == nil || *e.EVBps <= 0 {
		t.Errorf("takes %d, EV %v, want a positive mean EV over the taken signals", e.Takes, e.EVBps)
	}
	total := 0
	for _, b := range e.Calibration {
		total += b.Count
		if b.MeanPredicted < 0 || b.MeanPredicted > 1 || b.ObservedRate < 0 || b.ObservedRate > 1 {
			t.Errorf("bin %+v is outside [0, 1]", b)
		}
		if math.Abs(b.MeanPredicted-b.ObservedRate) > 0.15 && b.Count > 50 {
			t.Errorf("bin %+v is badly calibrated on the OOS rows", b)
		}
	}
	if total != e.Samples {
		t.Errorf("calibration bins hold %d rows, want %d", total, e.Samples)
	}
	// A higher bar for EV takes fewer signals.
	strict := training.Evaluate(m, rows, metalabel.DecideConfig{MinEVBps: 8, KellyFraction: metalabel.DefaultKellyFraction})
	if strict.Takes >= e.Takes {
		t.Errorf("min EV 8 takes %d, not fewer than %d", strict.Takes, e.Takes)
	}
	// No rows: nothing is defined.
	if empty := training.Evaluate(m, nil, metalabel.DefaultDecideConfig()); empty.Samples != 0 || empty.AUC != nil || empty.EVBps != nil || len(empty.Calibration) != 0 {
		t.Errorf("empty evaluation = %+v", empty)
	}
}

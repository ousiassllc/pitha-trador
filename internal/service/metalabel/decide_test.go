package metalabel_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

func constantModel(p, winBps, lossBps float64) *metalabel.Model {
	n := len(metalabel.FeatureNames())
	std := make([]float64, n)
	for i := range std {
		std[i] = 1
	}
	return &metalabel.Model{
		Version: "const", FeatureNames: metalabel.FeatureNames(),
		LR:         metalabel.LogisticRegression{Mean: make([]float64, n), Std: std, Weights: make([]float64, n)},
		Calibrator: metalabel.Calibrator{Method: metalabel.CalibrationPlatt, B: math.Log(p / (1 - p))},
		WinBps:     winBps, LossBps: lossBps,
	}
}

func TestDecide_TakesOnlyWhenCalibratedEVIsPositive(t *testing.T) {
	cfg := metalabel.DefaultDecideConfig()
	f := fullFeatures()

	// p=0.7, win 30, loss 30: EV = 12 bps, Kelly = 12/30, scaled by 0.25.
	d := metalabel.Decide(constantModel(0.7, 30, 30), domain.JevDirectionLong, f, cfg)
	if d.Action != repo.ActionTake || d.Reason != "" {
		t.Fatalf("decision = %+v, want take", d)
	}
	if math.Abs(d.ExpectedValueBps-12) > 1e-9 || math.Abs(d.CalibratedProbability-0.7) > 1e-9 || math.Abs(d.RawProbability-0.5) > 1e-9 {
		t.Errorf("probabilities/EV = %+v", d)
	}
	if math.Abs(d.SizeFraction-0.1) > 1e-9 {
		t.Errorf("SizeFraction = %v, want 0.1", d.SizeFraction)
	}
	if d.FeaturesJSON == "" || d.FeaturesJSON == "{}" {
		t.Errorf("FeaturesJSON = %q", d.FeaturesJSON)
	}

	// Break-even (p=0.5 with equal payoff) and negative EV are skips.
	for _, p := range []float64{0.5, 0.3} {
		d := metalabel.Decide(constantModel(p, 30, 30), domain.JevDirectionShort, f, cfg)
		if d.Action != repo.ActionSkip || d.SizeFraction != 0 || d.Reason != metalabel.ReasonEVBelowMin {
			t.Errorf("p=%v: decision = %+v, want skip(%s)", p, d, metalabel.ReasonEVBelowMin)
		}
	}
	// A skip still reports the EV that sank it.
	if d := metalabel.Decide(constantModel(0.3, 30, 30), domain.JevDirectionLong, f, cfg); d.ExpectedValueBps >= 0 {
		t.Errorf("skip EV = %v, want negative", d.ExpectedValueBps)
	}
}

func TestDecide_UsesTheCalibratedNotTheRawProbability(t *testing.T) {
	// Raw 0.5 is break-even for a 30/30 payoff, calibrated 0.7 is +12 bps: the
	// take proves EV comes from the calibrated value. The reverse (raw high,
	// calibrated low) is a skip.
	m := constantModel(0.7, 30, 30)
	if d := metalabel.Decide(m, domain.JevDirectionLong, fullFeatures(), metalabel.DefaultDecideConfig()); d.Action != repo.ActionTake {
		t.Fatalf("calibrated 0.7 over raw 0.5: %+v", d)
	}
	m.LR.Bias = 3 // raw ~0.95
	m.Calibrator.B = math.Log(0.4 / 0.6)
	if d := metalabel.Decide(m, domain.JevDirectionLong, fullFeatures(), metalabel.DefaultDecideConfig()); d.Action != repo.ActionSkip || d.RawProbability < 0.9 {
		t.Fatalf("raw 0.95 but calibrated 0.4: %+v", d)
	}
}

func TestDecide_MinEVAndSizeCap(t *testing.T) {
	m := constantModel(0.7, 30, 30) // EV 12 bps
	cfg := metalabel.DecideConfig{MinEVBps: 12, KellyFraction: 1}
	if d := metalabel.Decide(m, domain.JevDirectionLong, fullFeatures(), cfg); d.Action != repo.ActionSkip {
		t.Errorf("EV equal to the minimum must skip, got %+v", d)
	}
	cfg.MinEVBps = 11.9
	d := metalabel.Decide(m, domain.JevDirectionLong, fullFeatures(), cfg)
	if d.Action != repo.ActionTake || math.Abs(d.SizeFraction-0.4) > 1e-9 {
		t.Errorf("min 11.9, full Kelly: %+v, want take 0.4", d)
	}
	// An extremely favourable payoff cannot size above 1.
	huge := constantModel(0.99, 30, 0.1)
	if d := metalabel.Decide(huge, domain.JevDirectionLong, fullFeatures(), metalabel.DecideConfig{KellyFraction: 1}); d.Action != repo.ActionTake || d.SizeFraction > 1 {
		t.Errorf("size = %+v, want take capped at 1", d)
	}
}

func TestDecide_SkipsOnAnythingDoubtful(t *testing.T) {
	good := constantModel(0.9, 30, 30)
	cfg := metalabel.DefaultDecideConfig()

	missing := fullFeatures()
	missing.OFIMid = nil
	broken := *constantModel(0.9, 30, 30)
	broken.LR.Std = nil

	cases := map[string]struct {
		model  *metalabel.Model
		dir    string
		f      metalabel.Features
		cfg    metalabel.DecideConfig
		reason string
	}{
		"no model":            {nil, domain.JevDirectionLong, fullFeatures(), cfg, metalabel.ReasonNoModel},
		"no candidate":        {good, domain.JevDirectionNone, fullFeatures(), cfg, metalabel.ReasonNoCandidate},
		"missing feature":     {good, domain.JevDirectionLong, missing, cfg, metalabel.ReasonMissingFeature},
		"unusable model":      {&broken, domain.JevDirectionLong, fullFeatures(), cfg, metalabel.ReasonModelUnusable},
		"negative min EV":     {good, domain.JevDirectionLong, fullFeatures(), metalabel.DecideConfig{MinEVBps: -1, KellyFraction: 0.5}, metalabel.ReasonInvalidConfig},
		"zero kelly fraction": {good, domain.JevDirectionLong, fullFeatures(), metalabel.DecideConfig{}, metalabel.ReasonInvalidConfig},
	}
	for name, tc := range cases {
		d := metalabel.Decide(tc.model, tc.dir, tc.f, tc.cfg)
		if d.Action != repo.ActionSkip || d.SizeFraction != 0 || d.Reason != tc.reason {
			t.Errorf("%s: decision = %+v, want skip(%s)", name, d, tc.reason)
		}
	}
}

func TestDecideConfig_Validate(t *testing.T) {
	if err := metalabel.DefaultDecideConfig().Validate(); err != nil {
		t.Fatalf("default config: %v", err)
	}
	for name, c := range map[string]metalabel.DecideConfig{
		"NaN min EV":   {MinEVBps: math.NaN(), KellyFraction: 0.5},
		"negative":     {MinEVBps: -0.1, KellyFraction: 0.5},
		"kelly 0":      {KellyFraction: 0},
		"kelly above1": {KellyFraction: 1.1},
	} {
		if c.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, c)
		}
	}
}

package screener_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func f(v float64) *float64 { return &v }

// baseInput returns an Input that clears every default testCfg() filter,
// so each filter test only needs to break one field.
func baseInput() screener.Input {
	return screener.Input{
		InstrumentID:  1,
		Symbol:        "7203",
		Turnover5mJPY: 20_000_000,
		Snapshot: domain.Snapshot{
			Price:     1000,
			SpreadBps: f(10),
			Feature: domain.Feature{
				VolumeRatio5m: f(2.0),
				Return5m:      f(0.005), // decimal ratio: +0.5%
				RealizedVol5m: f(0.01),
			},
		},
	}
}

func testCfg() config.FastScreenerConfig {
	return config.FastScreenerConfig{
		MinPrice:              100,
		MaxPrice:              500000,
		MinTurnover5mJPY:      10_000_000,
		MaxSpreadBps:          50,
		MinVolumeRatio:        1.2,
		MinAbsReturn5mPct:     0.3,
		MinRealizedVolatility: 0.001,
		TopN:                  20,
		Weights: config.FastScreenerWeights{
			VolumeRatio:         0.2,
			AbsReturn5m:         0.2,
			BreakoutStrength:    0.2,
			OrderbookImbalance:  0.2,
			VolatilityExpansion: 0.2,
		},
	}
}

func TestPassesFilter_BaseInputPasses(t *testing.T) {
	if !screener.PassesFilter(testCfg(), baseInput()) {
		t.Fatal("expected baseInput() to pass every filter")
	}
}

func TestPassesFilter_RejectsOutOfScopeInstruments(t *testing.T) {
	tests := map[string]func(*screener.Input){
		"price below min":        func(in *screener.Input) { in.Snapshot.Price = 99 },
		"price above max":        func(in *screener.Input) { in.Snapshot.Price = 500001 },
		"turnover below min":     func(in *screener.Input) { in.Turnover5mJPY = 9_999_999 },
		"spread above max":       func(in *screener.Input) { in.Snapshot.SpreadBps = f(51) },
		"spread missing":         func(in *screener.Input) { in.Snapshot.SpreadBps = nil },
		"volume ratio below min": func(in *screener.Input) { in.Snapshot.Feature.VolumeRatio5m = f(1.19) },
		"volume ratio missing":   func(in *screener.Input) { in.Snapshot.Feature.VolumeRatio5m = nil },
		"abs return below min":   func(in *screener.Input) { in.Snapshot.Feature.Return5m = f(0.002) },
		"return missing":         func(in *screener.Input) { in.Snapshot.Feature.Return5m = nil },
		"negative return still passes abs check but fails vol": func(in *screener.Input) {
			in.Snapshot.Feature.Return5m = f(-0.002) // abs = 0.2% < 0.3%
		},
		"realized vol below min": func(in *screener.Input) { in.Snapshot.Feature.RealizedVol5m = f(0.0009) },
		"realized vol missing":   func(in *screener.Input) { in.Snapshot.Feature.RealizedVol5m = nil },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			in := baseInput()
			mutate(&in)
			if screener.PassesFilter(testCfg(), in) {
				t.Fatalf("expected filter to reject: %s", name)
			}
		})
	}
}

func TestPassesFilter_NegativeAbsReturnPasses(t *testing.T) {
	in := baseInput()
	in.Snapshot.Feature.Return5m = f(-0.005) // abs = 0.5% >= 0.3%
	if !screener.PassesFilter(testCfg(), in) {
		t.Fatal("expected abs(return_5m) to be evaluated, not signed return_5m")
	}
}

// Return5m is a Feature Engine decimal ratio (0.004 = +0.4%) while
// MinAbsReturn5mPct is in percent (0.3 = 0.3%): +0.4% must clear a 0.3%
// floor, and a 0.2% move must not (issue #364).
func TestPassesFilter_MinAbsReturn5mPctComparesPercentToDecimalRatio(t *testing.T) {
	tests := []struct {
		ratio float64
		want  bool
	}{
		{0.004, true},
		{-0.004, true},
		{0.003, true}, // == 0.3%: inclusive boundary
		{0.002, false},
		{-0.002, false},
		{0.3, true},
	}
	for _, tc := range tests {
		in := baseInput()
		in.Snapshot.Feature.Return5m = f(tc.ratio)
		if got := screener.PassesFilter(testCfg(), in); got != tc.want {
			t.Errorf("return_5m=%v (ratio) vs min_abs_return_5m_pct=0.3: pass=%v, want %v", tc.ratio, got, tc.want)
		}
	}
}

// End to end with real Feature Engine output (issue #364): a +0.4% five
// minute move clears the shipped 0.3% default, a +0.2% move does not.
func TestPassesFilter_FeatureEngineReturn5mAgainstDefaultThreshold(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		price float64
		want  bool
	}{{1004, true}, {996, true}, {1002, false}} {
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: now,
			Current:   featureengine.Reading{Price: tc.price, VWAP: tc.price},
			History:   []domain.Snapshot{{Timestamp: now.Add(-5 * time.Minute), Price: 1000}},
		})
		in := baseInput()
		in.Snapshot.Feature.Return5m = feature.Return5m
		if got := screener.PassesFilter(testCfg(), in); got != tc.want {
			t.Errorf("price %v vs ref 1000 (return_5m=%v): pass=%v, want %v", tc.price, *feature.Return5m, got, tc.want)
		}
	}
}

func TestPassesFilter_BoundaryValuesPass(t *testing.T) {
	in := baseInput()
	in.Snapshot.Price = 100 // == MinPrice
	if !screener.PassesFilter(testCfg(), in) {
		t.Fatal("expected price == MinPrice to pass (inclusive boundary)")
	}
	in.Snapshot.Price = 500000 // == MaxPrice
	if !screener.PassesFilter(testCfg(), in) {
		t.Fatal("expected price == MaxPrice to pass (inclusive boundary)")
	}
}

func TestScreenScore_SumsWeightedComponents(t *testing.T) {
	in := baseInput()
	in.Snapshot.Feature.VolumeRatio5m = f(2.0)
	in.Snapshot.Feature.Return5m = f(-0.5)
	in.BreakoutStrength = f(1.5)
	in.Snapshot.Feature.OrderbookImbalance = f(0.4)
	in.VolatilityExpansion = f(0.8)

	weights := config.FastScreenerWeights{
		VolumeRatio:         0.2,
		AbsReturn5m:         0.3,
		BreakoutStrength:    0.1,
		OrderbookImbalance:  0.15,
		VolatilityExpansion: 0.25,
	}
	// 0.2*2.0 + 0.3*abs(-0.5) + 0.1*1.5 + 0.15*0.4 + 0.25*0.8
	want := 0.2*2.0 + 0.3*0.5 + 0.1*1.5 + 0.15*0.4 + 0.25*0.8
	got := screener.ScreenScore(weights, in)
	if got != want {
		t.Fatalf("ScreenScore() = %v, want %v", got, want)
	}
}

func TestScreenScore_ExcludesNilComponentsRatherThanZero(t *testing.T) {
	in := baseInput()
	in.Snapshot.Feature.VolumeRatio5m = f(2.0)
	in.Snapshot.Feature.Return5m = f(0.5)
	in.BreakoutStrength = nil
	in.Snapshot.Feature.OrderbookImbalance = nil
	in.VolatilityExpansion = nil

	weights := testCfg().Weights
	// Only volume_ratio and abs_return_5m terms contribute.
	want := weights.VolumeRatio*2.0 + weights.AbsReturn5m*0.5
	got := screener.ScreenScore(weights, in)
	if got != want {
		t.Fatalf("ScreenScore() = %v, want %v (nil terms must be excluded, not zero)", got, want)
	}
}

func TestRun_FiltersScoresAndSelectsTopN(t *testing.T) {
	cfg := testCfg()
	cfg.TopN = 2

	mk := func(symbol string, volRatio float64) screener.Input {
		in := baseInput()
		in.Symbol = symbol
		in.Snapshot.Feature.VolumeRatio5m = f(volRatio)
		return in
	}

	rejected := baseInput()
	rejected.Symbol = "9999"
	rejected.Snapshot.Price = 50 // below MinPrice: rejected

	inputs := []screener.Input{
		mk("1000", 1.5), // lowest score among survivors
		mk("2000", 3.0), // highest score
		mk("3000", 2.0), // middle score
		rejected,
	}

	got := screener.Run(cfg, inputs)

	if len(got) != 2 {
		t.Fatalf("len(Run()) = %d, want 2 (TopN)", len(got))
	}
	if got[0].Symbol != "2000" || got[1].Symbol != "3000" {
		t.Fatalf("Run() order = [%s, %s], want [2000, 3000] (descending screen_score)",
			got[0].Symbol, got[1].Symbol)
	}
	if got[0].ScreenScore <= got[1].ScreenScore {
		t.Fatalf("Run() not sorted descending: %v then %v", got[0].ScreenScore, got[1].ScreenScore)
	}
	for _, c := range got {
		if c.JevDirection != nil || c.JevConfidence != nil || c.EntryQuality != nil {
			t.Fatalf("expected Jev* fields nil (Jev Scout is a later sub-scope), got %+v", c)
		}
		if c.CurrentPosition != nil {
			t.Fatalf("expected CurrentPosition nil (Execution is a later sub-scope), got %+v", c)
		}
	}
}

func TestRun_TopNLargerThanSurvivorsReturnsAll(t *testing.T) {
	cfg := testCfg()
	cfg.TopN = 20

	inputs := []screener.Input{baseInput(), baseInput()}
	got := screener.Run(cfg, inputs)
	if len(got) != 2 {
		t.Fatalf("len(Run()) = %d, want 2 (all survivors, TopN not exceeded)", len(got))
	}
}

func TestRun_CandidateFieldsMirrorSnapshot(t *testing.T) {
	cfg := testCfg()
	in := baseInput()
	in.InstrumentID = 42
	in.Symbol = "7203"
	in.Snapshot.Price = 1234.5
	in.Snapshot.Timestamp = time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	in.Snapshot.Feature.Return1m = f(0.05)
	in.Snapshot.Feature.PriceVsVWAPBps = 12.3

	got := screener.Run(cfg, []screener.Input{in})
	if len(got) != 1 {
		t.Fatalf("len(Run()) = %d, want 1", len(got))
	}
	c := got[0]
	if c.InstrumentID != 42 || c.Symbol != "7203" || c.Price != 1234.5 {
		t.Fatalf("Candidate identity fields = %+v, want InstrumentID=42 Symbol=7203 Price=1234.5", c)
	}
	if c.Return1m == nil || *c.Return1m != 0.05 {
		t.Fatalf("Candidate.Return1m = %v, want 0.05", c.Return1m)
	}
	if c.PriceVsVWAPBps != 12.3 {
		t.Fatalf("Candidate.PriceVsVWAPBps = %v, want 12.3", c.PriceVsVWAPBps)
	}
	if !c.AsOf.Equal(in.Snapshot.Timestamp) {
		t.Fatalf("Candidate.AsOf = %v, want %v", c.AsOf, in.Snapshot.Timestamp)
	}
}

package screener_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func TestFilterReasons_ReportsEveryFailedFilter(t *testing.T) {
	in := baseInput()
	in.Snapshot.Price = 50                     // < min_price
	in.Turnover5mJPY = 1                       // < min_turnover_5m_jpy
	in.Snapshot.SpreadBps = f(99)              // > max_spread_bps
	in.Snapshot.Feature.VolumeRatio5m = f(0.1) // < min_volume_ratio
	in.Snapshot.Feature.Return5m = f(-0.0001)  // |.| (0.01%) < min_abs_return_5m_pct (0.3%)
	in.Snapshot.Feature.RealizedVol5m = f(0)   // < min_realized_volatility
	got := screener.FilterReasons(testCfg(), in)
	want := []domain.ScreenReason{
		domain.ScreenReasonMinPrice, domain.ScreenReasonMinTurnover, domain.ScreenReasonMaxSpread,
		domain.ScreenReasonMinVolumeRatio, domain.ScreenReasonMinAbsReturn5m, domain.ScreenReasonMinRealizedVol,
	}
	if list := got.List(); len(list) != len(want) {
		t.Fatalf("reasons = %v, want %v", list, want)
	}
	for _, r := range want {
		if !got.Has(r) {
			t.Errorf("missing reason %s", r.Code())
		}
	}
	if got.Status() != domain.ScanStatusExcluded {
		t.Errorf("Status = %q, want excluded", got.Status())
	}
	if screener.FilterReasons(testCfg(), in) != got {
		t.Error("FilterReasons not deterministic")
	}
}

func TestFilterReasons_MaxPriceAndMissingValues(t *testing.T) {
	in := baseInput()
	in.Snapshot.Price = 600000
	if got := screener.FilterReasons(testCfg(), in); !got.Has(domain.ScreenReasonMaxPrice) || got.Status() != domain.ScanStatusExcluded {
		t.Fatalf("max price: %v", got.List())
	}

	in = baseInput()
	in.Snapshot.SpreadBps = nil
	in.Snapshot.Feature.VolumeRatio5m = nil
	in.Snapshot.Feature.Return5m = nil
	in.Snapshot.Feature.RealizedVol5m = nil
	in.Turnover5mJPY, in.TurnoverMissing = 0, true
	got := screener.FilterReasons(testCfg(), in)
	for _, r := range []domain.ScreenReason{
		domain.ScreenReasonMissingSpread, domain.ScreenReasonMissingVolumeRatio, domain.ScreenReasonMissingReturn5m,
		domain.ScreenReasonMissingRealizedVol, domain.ScreenReasonMissingTurnover,
	} {
		if !got.Has(r) {
			t.Errorf("missing reason %s", r.Code())
		}
	}
	if got.Has(domain.ScreenReasonMinTurnover) || got.Status() != domain.ScanStatusMissing {
		t.Errorf("missing values must not read as threshold failures: %v / %q", got.List(), got.Status())
	}
	// Unknown turnover still fails the liquidity floor (behavior unchanged).
	if screener.PassesFilter(testCfg(), in) {
		t.Error("unknown turnover passed the filter")
	}
}

func TestScreen_ReasonsParallelToInputsAndTopNCut(t *testing.T) {
	cfg := testCfg()
	cfg.TopN = 2
	mk := func(sym string, volRatio float64) screener.Input {
		in := baseInput()
		in.Symbol = sym
		in.Snapshot.Feature.VolumeRatio5m = f(volRatio)
		return in
	}
	bad := baseInput()
	bad.Symbol = "BAD"
	bad.Snapshot.Price = 1
	inputs := []screener.Input{mk("LOW", 1.3), bad, mk("HIGH", 9), mk("MID", 5)}
	cfg.Weights.VolumeRatio = 1

	res := screener.Screen(cfg, inputs)
	if len(res.Candidates) != 2 || res.Candidates[0].Symbol != "HIGH" || res.Candidates[1].Symbol != "MID" {
		t.Fatalf("candidates = %+v", res.Candidates)
	}
	if len(res.Reasons) != len(inputs) {
		t.Fatalf("len(Reasons) = %d, want %d", len(res.Reasons), len(inputs))
	}
	if res.Reasons[0] != domain.ScreenReasons(0).Add(domain.ScreenReasonRankedOut) {
		t.Errorf("LOW reasons = %v, want only top_n_cutoff", res.Reasons[0].List())
	}
	if !res.Reasons[1].Has(domain.ScreenReasonMinPrice) || res.Reasons[1].Has(domain.ScreenReasonRankedOut) {
		t.Errorf("BAD reasons = %v", res.Reasons[1].List())
	}
	if res.Reasons[2] != 0 || res.Reasons[3] != 0 {
		t.Errorf("candidates must have no reasons: %v %v", res.Reasons[2].List(), res.Reasons[3].List())
	}
	if got := screener.Run(cfg, inputs); len(got) != 2 || got[0].Symbol != "HIGH" {
		t.Errorf("Run diverged from Screen: %+v", got)
	}
}

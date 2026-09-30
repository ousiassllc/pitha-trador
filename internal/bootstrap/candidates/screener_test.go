package candidates

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func lenientFastScreener() config.FastScreenerConfig {
	return config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 100,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}
}

func TestRefresh_ScreenScoreIncludesBreakoutStrengthFromSnapshotHistory(t *testing.T) {
	refresher := newTestRefresher(t)
	cfg := lenientFastScreener()
	cfg.Weights = config.FastScreenerWeights{BreakoutStrength: 1}
	refresher.Strategy.FastScreener = cfg

	inst := mustCreateInstrument(t, refresher, "7203")
	now := time.Now().UTC().Truncate(time.Minute)
	bar := func(minutesAgo int, price float64) domain.Snapshot {
		return domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol,
			Timestamp: now.Add(-time.Duration(minutesAgo) * time.Minute),
			Price:     price, Volume: 1000, Turnover: 1_000_000, SpreadBps: ptrF(10),
			Feature: domain.Feature{
				VWAP: price, VolumeRatio5m: ptrF(1), Return5m: ptrF(0.01), RealizedVol5m: ptrF(0.01),
			},
		}
	}
	// Prior 5 minutes peak at 2000; the latest bar prints 2100.
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{
		bar(3, 1990), bar(2, 2000), bar(1, 1995), bar(0, 2100),
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	candidates, _, err := refresher.Screener.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if want := 2100.0/2000.0 - 1; candidates[0].ScreenScore < want-1e-9 || candidates[0].ScreenScore > want+1e-9 {
		t.Errorf("ScreenScore = %v, want %v (weight 1 * breakout_strength)", candidates[0].ScreenScore, want)
	}
}

func TestRefresh_RuntimeSettingsOverrideStrategyFilters(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.Strategy.FastScreener = lenientFastScreener()

	inst := mustCreateInstrument(t, refresher, "7203")
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{
			VWAP: 2490, VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
		},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	ctx := context.Background()

	count := func() int {
		t.Helper()
		if err := refresher.Refresh(ctx); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		candidates, _, err := refresher.Screener.Candidates(ctx)
		if err != nil {
			t.Fatalf("Candidates: %v", err)
		}
		return len(candidates)
	}

	if got := count(); got != 1 {
		t.Fatalf("baseline candidates = %d, want 1", got)
	}

	// FR-FS-3: a DB value raises min_price above the price and takes
	// effect on the very next refresh, without touching refresher.Strategy.
	if err := refresher.Settings.Set(ctx, "screener.min_price", "3000", time.Now().UTC()); err != nil {
		t.Fatalf("Settings.Set: %v", err)
	}
	if got := count(); got != 0 {
		t.Errorf("candidates with screener.min_price=3000 = %d, want 0", got)
	}

	if err := refresher.Settings.Set(ctx, "screener.min_price", "1000", time.Now().UTC()); err != nil {
		t.Fatalf("Settings.Set: %v", err)
	}
	if got := count(); got != 1 {
		t.Errorf("candidates with screener.min_price=1000 = %d, want 1", got)
	}
}

func TestRefresh_ReturnsErrorForMalformedRuntimeSetting(t *testing.T) {
	refresher := newTestRefresher(t)
	if err := refresher.Settings.Set(context.Background(), "screener.top_n", `"many"`, time.Now().UTC()); err != nil {
		t.Fatalf("Settings.Set: %v", err)
	}

	if err := refresher.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh returned nil error, want error for a non-numeric screener.top_n")
	}
}

// Regression for #163: turnover_5m is a cumulative difference, not a sum.
func TestRefresh_Turnover5mIsCumulativeDifference(t *testing.T) {
	refresher := newTestRefresher(t)
	cfg := lenientFastScreener()
	cfg.MinTurnover5mJPY = 30_000_000
	refresher.Strategy.FastScreener = cfg

	now := time.Now().UTC().Truncate(time.Minute)
	series := func(inst domain.Instrument, cumulativeAt func(minutesAgo int) float64) {
		var bars []domain.Snapshot
		for m := 6; m >= 0; m-- {
			bars = append(bars, domain.Snapshot{
				InstrumentID: inst.ID, Symbol: inst.Symbol,
				Timestamp: now.Add(-time.Duration(m) * time.Minute), Price: 2000, Volume: 1000,
				Turnover: cumulativeAt(m), SpreadBps: ptrF(10),
				Feature: domain.Feature{VWAP: 2000, VolumeRatio5m: ptrF(1), Return5m: ptrF(0.01), RealizedVol5m: ptrF(0.01)},
			})
		}
		if _, err := refresher.Snapshots.InsertBatch(context.Background(), bars); err != nil {
			t.Fatalf("InsertBatch: %v", err)
		}
	}
	// Illiquid: cumulative 1e9, nothing traded (a 5-bar sum reads 5e9).
	series(mustCreateInstrument(t, refresher, "1111"), func(int) float64 { return 1e9 })
	liquid := func(m int) float64 { return 1e9 - float64(m)*10e6 }
	series(mustCreateInstrument(t, refresher, "2222"), liquid)
	// Index instruments are never screened.
	topix, err := refresher.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: "TOPIX", Name: "TOPIX", Market: "TSE", Kind: domain.InstrumentKindMarketIndex, IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	series(topix, liquid)

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	candidates, _, err := refresher.Screener.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Symbol != "2222" {
		t.Fatalf("candidates = %+v, want only the liquid stock 2222", candidates)
	}
}

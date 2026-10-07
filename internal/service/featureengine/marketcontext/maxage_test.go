package marketcontext_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/marketcontext"
)

// fullScanAge is the shipped scan.full_scan_max_snapshot_age_seconds (620):
// one full REST cycle of ~4,000 symbols plus a margin (issue #686).
const fullScanAge = 620 * time.Second

// Regression for #692: the allowed bar age used to be a fixed 3 minutes, so
// with scan.full_scan_enabled: true (index rows refreshed once per ~8 minute
// full REST cycle) market_return_1m/5m, sector_return_5m and market_breadth
// were missing for most of the cycle. The age is now injected: the Loader
// accepts a bar exactly as old as maxStale and ignores anything older, for
// the ranking-watch 3 minutes (unchanged) and the full-scan age alike.
func TestLoader_MaxStaleFollowsInjectedAge(t *testing.T) {
	at := time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)
	for _, mode := range []struct {
		name     string
		maxStale time.Duration
	}{
		{"ranking watch", domain.MaxSnapshotAge},
		{"full scan", fullScanAge},
	} {
		for _, age := range []time.Duration{
			time.Minute, 179 * time.Second, 180 * time.Second, 181 * time.Second,
			300 * time.Second, 620 * time.Second, 621 * time.Second,
		} {
			t.Run(fmt.Sprintf("%s/bar age %v", mode.name, age), func(t *testing.T) {
				f := newLoaderFixtureWithAge(t, mode.maxStale)
				sector := "Auto"
				stock := f.instrument(t, "7203", domain.InstrumentKindStock, &sector)
				topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
				autoIdx := f.instrument(t, "AUTO", domain.InstrumentKindSectorIndex, &sector)
				other := f.instrument(t, "6758", domain.InstrumentKindStock, nil)

				latest := at.Add(-age)
				idxBars := func(then, now float64) []domain.Snapshot {
					return []domain.Snapshot{
						{Timestamp: latest.Add(-5 * time.Minute), Price: then},
						{Timestamp: latest.Add(-time.Minute), Price: then},
						{Timestamp: latest, Price: now},
					}
				}
				f.bars(t, topix, idxBars(1000, 1010)...)
				f.bars(t, autoIdx, idxBars(500, 505)...)
				f.bars(t, other, domain.Snapshot{Timestamp: latest, Price: 100, Feature: domain.Feature{Return5m: ptr(0.01)}})

				mc := f.loader.Load(context.Background(), stock, at)
				if age <= mode.maxStale {
					wantValue(t, "MarketReturn5m", mc.MarketReturn5m, 0.01)
					wantValue(t, "MarketReturn1m", mc.MarketReturn1m, 0.01)
					wantValue(t, "SectorReturn5m", mc.SectorReturn5m, 0.01)
					wantValue(t, "MarketBreadth", mc.MarketBreadth, 1)
					return
				}
				if mc.MarketReturn5m != nil || mc.MarketReturn1m != nil || mc.SectorReturn5m != nil || mc.MarketBreadth != nil {
					t.Errorf("context = %+v, want all nil for a bar older than %v", mc, mode.maxStale)
				}
			})
		}
	}
}

// A non-positive age (a Handler that was never given one) behaves like the
// ranking-watch domain.MaxSnapshotAge rather than ignoring every bar.
func TestNewLoader_NonPositiveAgeFallsBackToRankingWatchAge(t *testing.T) {
	at := time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)
	f := newLoaderFixtureWithAge(t, 0)
	stock := f.instrument(t, "7203", domain.InstrumentKindStock, nil)
	topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
	f.bars(t, topix,
		domain.Snapshot{Timestamp: at.Add(-domain.MaxSnapshotAge - 5*time.Minute), Price: 1000},
		domain.Snapshot{Timestamp: at.Add(-domain.MaxSnapshotAge), Price: 1010})
	wantValue(t, "MarketReturn5m", f.loader.Load(context.Background(), stock, at).MarketReturn5m, 0.01)
}

// cacheTTL must stay well below the shortest allowed age (the ranking-watch
// domain.MaxSnapshotAge) so a cached value is never served long enough to
// outlive the bars it was computed from (issue #692).
func TestCacheTTL_WellBelowAllowedAge(t *testing.T) {
	if marketcontext.CacheTTL*4 > domain.MaxSnapshotAge {
		t.Errorf("cacheTTL = %v, want at most a quarter of domain.MaxSnapshotAge (%v)", marketcontext.CacheTTL, domain.MaxSnapshotAge)
	}
}

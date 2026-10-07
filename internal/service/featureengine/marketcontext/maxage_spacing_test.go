package marketcontext_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Documents the full-scan limitation (issues #693/#694/#696): with
// scan.full_scan_enabled: true each index row gets one bar per ~8 minute full
// REST cycle. The 620s max-age (scan.full_scan_max_snapshot_age_seconds) only
// widens the latest-bar freshness check; the window reference of
// market_return_1m/5m and sector_return_5m is still bound by FR-FE-5's
// max(window/2, 90s) tolerance, so those values stay nil, and
// market_breadth stays nil because the stocks' own return_5m is nil too. This
// is the specified behaviour (FR-FE-4), not a bug: the ranking-watch default
// is the supported mode.
func TestLoader_FullScanBarSpacingLeavesContextNilDespiteWideMaxStale(t *testing.T) {
	at := time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)
	f := newLoaderFixtureWithAge(t, fullScanAge)
	sector := "Auto"
	stock := f.instrument(t, "7203", domain.InstrumentKindStock, &sector)
	topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
	autoIdx := f.instrument(t, "AUTO", domain.InstrumentKindSectorIndex, &sector)
	other := f.instrument(t, "6758", domain.InstrumentKindStock, nil)

	latest := at.Add(-30 * time.Second) // fresh: well inside the 620s max age
	const spacing = 8 * time.Minute
	idxBars := func(oldest, mid, now float64) []domain.Snapshot {
		return []domain.Snapshot{
			{Timestamp: latest.Add(-2 * spacing), Price: oldest},
			{Timestamp: latest.Add(-spacing), Price: mid},
			{Timestamp: latest, Price: now},
		}
	}
	f.bars(t, topix, idxBars(990, 1000, 1010)...)
	f.bars(t, autoIdx, idxBars(495, 500, 505)...)
	// return_5m is nil for a stock with 8-minute-spaced bars (FR-FE-5).
	f.bars(t, other, domain.Snapshot{Timestamp: latest, Price: 100})

	mc := f.loader.Load(context.Background(), stock, at)
	if mc.MarketReturn1m != nil || mc.MarketReturn5m != nil || mc.SectorReturn5m != nil || mc.MarketBreadth != nil {
		t.Errorf("context = %+v, want all nil with %v-spaced bars", mc, spacing)
	}
}

package marketcontext_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/marketcontext"
)

// countingSources counts the repository reads a Loader issues.
type countingSources struct {
	f                                      loaderFixture
	listByKind, stockReturns, historyReads int
}

func (c *countingSources) ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error) {
	c.listByKind++
	return c.f.instruments.ListActiveByKind(ctx, kind)
}

func (c *countingSources) LatestStockReturns5m(ctx context.Context, since, until time.Time) ([]float64, error) {
	c.stockReturns++
	return c.f.instruments.LatestStockReturns5m(ctx, since, until)
}

func (c *countingSources) ListHistoryByInstrument(ctx context.Context, id int64, limit int) ([]domain.Snapshot, error) {
	c.historyReads++
	return c.f.snapshots.ListHistoryByInstrument(ctx, id, limit)
}

// Regression for #622: every market-data job re-read the index history and
// the whole stock universe. Loads of different stocks within one scan cycle
// must share one market-wide computation.
func TestLoader_SharesMarketWideValuesAcrossStocks(t *testing.T) {
	f := newLoaderFixture(t)
	topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
	autoIdx := f.instrument(t, "AUTO", domain.InstrumentKindSectorIndex, strPtr("Auto"))
	var stocks []domain.Instrument
	for _, sym := range []string{"7203", "7267", "7201", "6758"} {
		stocks = append(stocks, f.instrument(t, sym, domain.InstrumentKindStock, strPtr("Auto")))
	}
	now := time.Now().UTC()
	idxBars := func(then, latest float64) []domain.Snapshot {
		return []domain.Snapshot{
			{Timestamp: now.Add(-6 * time.Minute), Price: then},
			{Timestamp: now.Add(-1 * time.Minute), Price: latest},
		}
	}
	f.bars(t, topix, idxBars(1000, 1010)...)
	f.bars(t, autoIdx, idxBars(500, 505)...)

	src := &countingSources{f: f}
	loader := marketcontext.NewLoader(src, src)
	for i, stock := range stocks {
		mc := loader.Load(context.Background(), stock, now.Add(time.Duration(i)*time.Second))
		wantValue(t, stock.Symbol+" MarketReturn5m", mc.MarketReturn5m, 0.01)
		wantValue(t, stock.Symbol+" SectorReturn5m", mc.SectorReturn5m, 0.01)
	}
	// market: 1 market-index list + 1 stock-returns; sector: 1 list. History
	// reads: 1 TOPIX + 1 sector index.
	if src.listByKind != 2 || src.stockReturns != 1 || src.historyReads != 2 {
		t.Errorf("reads for %d stocks = list %d, stockReturns %d, history %d; want 2, 1, 2",
			len(stocks), src.listByKind, src.stockReturns, src.historyReads)
	}
}

func TestLoader_RecomputesAfterTTLAndNeverServesFutureValues(t *testing.T) {
	f := newLoaderFixture(t)
	stock := f.instrument(t, "7203", domain.InstrumentKindStock, nil)
	src := &countingSources{f: f}
	loader := marketcontext.NewLoader(src, src)
	ctx := context.Background()
	now := time.Now().UTC()

	loader.Load(ctx, stock, now)
	loader.Load(ctx, stock, now.Add(10*time.Second))
	if src.stockReturns != 1 {
		t.Fatalf("stockReturns within TTL = %d, want 1", src.stockReturns)
	}
	loader.Load(ctx, stock, now.Add(time.Minute))
	if src.stockReturns != 2 {
		t.Errorf("stockReturns after TTL = %d, want 2", src.stockReturns)
	}
	// A value computed at a later instant must not serve an earlier at
	// (replay / backfill must not see the future: FR-FE-1).
	loader.Load(ctx, stock, now)
	if src.stockReturns != 3 {
		t.Errorf("stockReturns for an earlier at = %d, want 3", src.stockReturns)
	}
}

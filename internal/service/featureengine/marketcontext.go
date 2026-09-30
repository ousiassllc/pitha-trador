package featureengine

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// marketContextMaxStale is how old an index instrument's latest bar (or a
// stock's latest bar, for market_breadth) may be before it is ignored
// rather than read as the current market: three full-scan intervals.
const marketContextMaxStale = 3 * time.Minute

// MarketContext is the functional.md §4.1 市場コンテキスト input set for
// one stock's bar. Any field is nil when its source is unavailable
// (FR-FE-2).
type MarketContext struct {
	MarketReturn1m *float64
	MarketReturn5m *float64
	SectorReturn5m *float64
	MarketBreadth  *float64
}

// MarketContextLoader reads the tracked index instruments and the stock
// universe from the database to build each stock's MarketContext.
type MarketContextLoader struct {
	instruments *market.InstrumentRepository
	snapshots   *market.SnapshotRepository
}

// NewMarketContextLoader returns a loader reading via the given repositories.
func NewMarketContextLoader(instruments *market.InstrumentRepository, snapshots *market.SnapshotRepository) *MarketContextLoader {
	return &MarketContextLoader{instruments: instruments, snapshots: snapshots}
}

// Load derives inst's market context at time at from the
// tracked index instruments (instruments.kind = market_index for
// TOPIX/Nikkei225, sector_index matched on Sector for sector_return_5m)
// and the stock universe's latest 5-minute returns (market_breadth). Index
// instruments themselves get an empty context. A failing source is logged
// and left nil so one broken lookup cannot stop the stock's own bar from
// being recorded.
func (l *MarketContextLoader) Load(ctx context.Context, inst domain.Instrument, at time.Time) MarketContext {
	if inst.Kind != domain.InstrumentKindStock {
		return MarketContext{}
	}

	var mc MarketContext
	markets, err := l.instruments.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex)
	if err != nil {
		slog.Warn("bootstrap: list market index instruments", "error", err)
	}
	var r1m, r5m []*float64
	for _, idx := range markets {
		bars := l.indexBars(ctx, idx)
		r1m = append(r1m, IndexReturn(at, bars, time.Minute, marketContextMaxStale))
		r5m = append(r5m, IndexReturn(at, bars, 5*time.Minute, marketContextMaxStale))
	}
	mc.MarketReturn1m = MeanReturn(r1m)
	mc.MarketReturn5m = MeanReturn(r5m)

	if inst.Sector != nil {
		sectors, err := l.instruments.ListActiveByKind(ctx, domain.InstrumentKindSectorIndex)
		if err != nil {
			slog.Warn("bootstrap: list sector index instruments", "error", err)
		}
		var sr []*float64
		for _, idx := range sectors {
			if idx.Sector != nil && *idx.Sector == *inst.Sector {
				sr = append(sr, IndexReturn(at, l.indexBars(ctx, idx), 5*time.Minute, marketContextMaxStale))
			}
		}
		mc.SectorReturn5m = MeanReturn(sr)
	}

	returns, err := l.instruments.LatestStockReturns5m(ctx, at.Add(-marketContextMaxStale), at)
	if err != nil {
		slog.Warn("bootstrap: list stock returns for market breadth", "error", err)
	}
	mc.MarketBreadth = Breadth(returns)
	return mc
}

// indexBars returns idx's recent bars, or nil (logged) on a read error.
func (l *MarketContextLoader) indexBars(ctx context.Context, idx domain.Instrument) []domain.Snapshot {
	bars, err := l.snapshots.ListByInstrument(ctx, idx.ID, HistoryLookbackBars)
	if err != nil {
		slog.Warn("bootstrap: list index snapshots", "symbol", idx.Symbol, "error", err)
		return nil
	}
	return bars
}

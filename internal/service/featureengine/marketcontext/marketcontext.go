// Package marketcontext derives the functional.md §4.1 市場コンテキスト
// inputs (market/sector index returns, market breadth) that Feature Engine
// feeds into every stock's bar. The values are shared by all stocks, so
// Loader computes them once per cacheTTL instead of once per market-data job
// (issue #622). It lives in its own package to keep featureengine under the
// linterly line budget.
package marketcontext

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// maxStale is how old an index instrument's latest bar (or a stock's latest
// bar, for market_breadth) may be before it is ignored rather than read as
// the current market: three full-scan intervals.
const maxStale = 3 * time.Minute

// cacheTTL is how long a computed market-wide value (or one sector's value)
// is served to later Load calls. It is far below maxStale and below the
// 1-minute bar interval, so cached values stay current while every stock of
// a scan cycle shares one read of the index history and the stock universe.
const cacheTTL = 30 * time.Second

// Context is the functional.md §4.1 市場コンテキスト input set for one
// stock's bar. Any field is nil when its source is unavailable (FR-FE-2).
type Context struct {
	MarketReturn1m *float64
	MarketReturn5m *float64
	SectorReturn5m *float64
	MarketBreadth  *float64
}

// Instruments is the instrument-repository subset Loader reads
// (*market.InstrumentRepository).
type Instruments interface {
	ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error)
	LatestStockReturns5m(ctx context.Context, since, until time.Time) ([]float64, error)
}

// Snapshots is the snapshot-repository subset Loader reads
// (*market.SnapshotRepository).
type Snapshots interface {
	ListHistoryByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.Snapshot, error)
}

// marketLevel is the market-wide part of a Context computed at one instant.
type marketLevel struct {
	at                          time.Time
	return1m, return5m, breadth *float64
}

// sectorLevel is one sector index' 5-minute return computed at one instant.
type sectorLevel struct {
	at       time.Time
	return5m *float64
}

// Loader reads the tracked index instruments and the stock universe from the
// database to build each stock's Context. It is safe for concurrent use.
type Loader struct {
	instruments Instruments
	snapshots   Snapshots

	mu      sync.Mutex
	market  *marketLevel
	sectors map[string]sectorLevel
}

// NewLoader returns a Loader reading via the given repositories.
func NewLoader(instruments Instruments, snapshots Snapshots) *Loader {
	return &Loader{instruments: instruments, snapshots: snapshots, sectors: map[string]sectorLevel{}}
}

// fresh reports whether a value computed at computedAt may serve a Load at
// at: it was computed no later than at (FR-FE-1 look-ahead guard: it never
// saw a bar newer than at) and less than cacheTTL earlier.
func fresh(computedAt, at time.Time) bool {
	return !computedAt.After(at) && at.Sub(computedAt) < cacheTTL
}

// Load derives inst's market context at time at from the tracked index
// instruments (instruments.kind = market_index for TOPIX/Nikkei225,
// sector_index matched on Sector for sector_return_5m) and the stock
// universe's latest 5-minute returns (market_breadth). Index instruments
// themselves get an empty context. A failing source is logged and left nil
// (and not cached) so one broken lookup cannot stop the stock's own bar from
// being recorded.
func (l *Loader) Load(ctx context.Context, inst domain.Instrument, at time.Time) Context {
	if inst.Kind != domain.InstrumentKindStock {
		return Context{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	m := l.marketLevel(ctx, at)
	mc := Context{MarketReturn1m: m.return1m, MarketReturn5m: m.return5m, MarketBreadth: m.breadth}
	if inst.Sector != nil {
		mc.SectorReturn5m = l.sectorReturn5m(ctx, *inst.Sector, at)
	}
	return mc
}

// marketLevel returns the cached market-wide values when still fresh for at,
// else recomputes them (index returns and breadth).
func (l *Loader) marketLevel(ctx context.Context, at time.Time) marketLevel {
	if l.market != nil && fresh(l.market.at, at) {
		return *l.market
	}
	m := marketLevel{at: at}
	ok := true

	markets, err := l.instruments.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex)
	if err != nil {
		slog.Warn("bootstrap: list market index instruments", "error", err)
		ok = false
	}
	var r1m, r5m []*float64
	for _, idx := range markets {
		bars, barsOK := l.indexBars(ctx, idx)
		ok = ok && barsOK
		r1m = append(r1m, featureengine.IndexReturn(at, bars, time.Minute, maxStale))
		r5m = append(r5m, featureengine.IndexReturn(at, bars, 5*time.Minute, maxStale))
	}
	m.return1m = featureengine.MeanReturn(r1m)
	m.return5m = featureengine.MeanReturn(r5m)

	returns, err := l.instruments.LatestStockReturns5m(ctx, at.Add(-maxStale), at)
	if err != nil {
		slog.Warn("bootstrap: list stock returns for market breadth", "error", err)
		ok = false
	}
	m.breadth = featureengine.Breadth(returns)

	if ok {
		l.market = &m
	}
	return m
}

// sectorReturn5m returns the cached 5-minute return of sector's index when
// still fresh for at, else recomputes it.
func (l *Loader) sectorReturn5m(ctx context.Context, sector string, at time.Time) *float64 {
	if s, found := l.sectors[sector]; found && fresh(s.at, at) {
		return s.return5m
	}
	ok := true
	sectors, err := l.instruments.ListActiveByKind(ctx, domain.InstrumentKindSectorIndex)
	if err != nil {
		slog.Warn("bootstrap: list sector index instruments", "error", err)
		ok = false
	}
	var sr []*float64
	for _, idx := range sectors {
		if idx.Sector != nil && *idx.Sector == sector {
			bars, barsOK := l.indexBars(ctx, idx)
			ok = ok && barsOK
			sr = append(sr, featureengine.IndexReturn(at, bars, 5*time.Minute, maxStale))
		}
	}
	result := featureengine.MeanReturn(sr)
	if ok {
		l.sectors[sector] = sectorLevel{at: at, return5m: result}
	}
	return result
}

// indexBars returns idx's recent bars; ok is false (and the failure logged)
// on a read error.
func (l *Loader) indexBars(ctx context.Context, idx domain.Instrument) (bars []domain.Snapshot, ok bool) {
	bars, err := l.snapshots.ListHistoryByInstrument(ctx, idx.ID, featureengine.HistoryLookbackBars)
	if err != nil {
		slog.Warn("bootstrap: list index snapshots", "symbol", idx.Symbol, "error", err)
		return nil, false
	}
	return bars, true
}

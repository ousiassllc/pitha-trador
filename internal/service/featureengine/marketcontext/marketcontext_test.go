package marketcontext_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/marketcontext"
)

type loaderFixture struct {
	instruments *market.InstrumentRepository
	snapshots   *market.SnapshotRepository
	loader      *marketcontext.Loader
}

// newLoaderFixture is a ranking-watch Loader (domain.MaxSnapshotAge).
func newLoaderFixture(t *testing.T) loaderFixture {
	t.Helper()
	return newLoaderFixtureWithAge(t, domain.MaxSnapshotAge)
}

func newLoaderFixtureWithAge(t *testing.T, maxStale time.Duration) loaderFixture {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	instruments, snapshots := market.NewInstrumentRepository(db), market.NewSnapshotRepository(db)
	return loaderFixture{instruments, snapshots, marketcontext.NewLoader(instruments, snapshots, maxStale)}
}

func (f loaderFixture) instrument(t *testing.T, symbol, kind string, sector *string) domain.Instrument {
	t.Helper()
	inst, err := f.instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol, Market: "TSE Prime", Sector: sector, Kind: kind, IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func (f loaderFixture) bars(t *testing.T, inst domain.Instrument, bars ...domain.Snapshot) {
	t.Helper()
	for i := range bars {
		bars[i].InstrumentID, bars[i].Symbol = inst.ID, inst.Symbol
	}
	if _, err := f.snapshots.InsertBatch(context.Background(), bars); err != nil {
		t.Fatalf("InsertBatch %s: %v", inst.Symbol, err)
	}
}

func strPtr(s string) *string { return &s }

func ptr(v float64) *float64 { return &v }

func wantValue(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if math.Abs(*got-want) >= 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

// Regression for #144: market_return_1m/5m, sector_return_5m and
// market_breadth were never populated.
func TestLoader_DerivesContextFromTrackedIndices(t *testing.T) {
	f := newLoaderFixture(t)
	sector := "Transportation Equipment"
	stock := f.instrument(t, "7203", domain.InstrumentKindStock, &sector)
	other := f.instrument(t, "6758", domain.InstrumentKindStock, nil)
	topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
	nikkei := f.instrument(t, "N225", domain.InstrumentKindMarketIndex, nil)
	autoIdx := f.instrument(t, "AUTO", domain.InstrumentKindSectorIndex, &sector)
	bankIdx := f.instrument(t, "BANK", domain.InstrumentKindSectorIndex, strPtr("Banks"))

	now := time.Now().UTC()
	idxBars := func(then, latest float64) []domain.Snapshot {
		return []domain.Snapshot{
			{Timestamp: now.Add(-6 * time.Minute), Price: then},
			{Timestamp: now.Add(-2 * time.Minute), Price: then}, // 1m reference for the -1m bar
			{Timestamp: now.Add(-1 * time.Minute), Price: latest},
		}
	}
	f.bars(t, topix, idxBars(1000, 1010)...)  // +1.0% over 5m
	f.bars(t, nikkei, idxBars(2000, 2040)...) // +2.0%
	f.bars(t, autoIdx, idxBars(500, 505)...)  // +1.0%
	f.bars(t, bankIdx, idxBars(100, 200)...)  // wrong sector: must not be used
	f.bars(t, other, domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 100, Feature: domain.Feature{Return5m: ptr(0.01)}})

	mc := f.loader.Load(context.Background(), stock, now)
	wantValue(t, "MarketReturn5m", mc.MarketReturn5m, 0.015) // mean of TOPIX and Nikkei
	wantValue(t, "SectorReturn5m", mc.SectorReturn5m, 0.01)
	wantValue(t, "MarketBreadth", mc.MarketBreadth, 1)
	if mc.MarketReturn1m == nil {
		t.Error("MarketReturn1m = nil, want a value")
	}
}

func TestLoader_MissingSourcesAreNil(t *testing.T) {
	f := newLoaderFixture(t)
	stock := f.instrument(t, "7203", domain.InstrumentKindStock, strPtr("Banks"))
	mc := f.loader.Load(context.Background(), stock, time.Now().UTC())
	if mc.MarketReturn1m != nil || mc.MarketReturn5m != nil || mc.SectorReturn5m != nil || mc.MarketBreadth != nil {
		t.Errorf("context = %+v, want all nil with no tracked instruments (FR-FE-2)", mc)
	}

	// A stalled index feed is not read as the current market.
	topix := f.instrument(t, "TOPIX", domain.InstrumentKindMarketIndex, nil)
	old := time.Now().UTC().Add(-time.Hour)
	f.bars(t, topix,
		domain.Snapshot{Timestamp: old.Add(-6 * time.Minute), Price: 1000},
		domain.Snapshot{Timestamp: old, Price: 1010})
	if mc := f.loader.Load(context.Background(), stock, time.Now().UTC()); mc.MarketReturn5m != nil {
		t.Errorf("MarketReturn5m = %v, want nil for a stale index", *mc.MarketReturn5m)
	}

	if mc := f.loader.Load(context.Background(), topix, time.Now().UTC()); mc != (marketcontext.Context{}) {
		t.Errorf("index instrument context = %+v, want empty", mc)
	}
}

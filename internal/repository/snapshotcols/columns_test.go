package snapshotcols_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// fullFeature sets every nullable Feature field to a distinct value so a
// column swapped or dropped by the persistence mapping shows up.
func fullFeature() domain.Feature {
	v := 0.0
	next := func() *float64 { v += 0.001; x := v; return &x }
	i1, i2, dir := int64(1200), int64(6000), int64(-1)
	return domain.Feature{
		Return1m: next(), Return3m: next(), Return5m: next(), Return15m: next(), Return30m: next(),
		HighDistance5m: next(), LowDistance5m: next(), SessionHighDistance: next(), SessionLowDistance: next(),
		VWAP: 2498, PriceVsVWAPBps: 10, VWAPSlope: next(), VWAPCrossDirection: &dir,
		Volume1m: &i1, Volume5m: &i2, VolumeRatio1m: next(), VolumeRatio5m: next(), Turnover1m: next(), Turnover5m: next(),
		ATR1m: next(), ATR5m: next(), RealizedVol5m: next(), RealizedVol15m: next(), VolatilityExpansionRatio: next(),
		BidDepth: next(), AskDepth: next(), OrderbookImbalance: next(), BuyTradeRatio: next(), SellTradeRatio: next(),
		TradeFlowImbalance: next(), Microprice: next(),
		MarketReturn1m: next(), MarketReturn5m: next(), SectorReturn5m: next(),
		StockVsSectorRelativeStrength: next(), MarketBreadth: next(),
	}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func mustInstrument(t *testing.T, conn *sql.DB) domain.Instrument {
	t.Helper()
	inst, err := market.NewInstrumentRepository(conn).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	return inst
}

func TestColumns_RoundTripEveryFeatureField(t *testing.T) {
	conn := openDB(t)
	inst := mustInstrument(t, conn)
	repo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	want := fullFeature()
	saved, err := repo.Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Date(2026, 9, 26, 1, 15, 0, 0, time.UTC),
		Price: 2500, Volume: 1, Turnover: 1, Feature: want,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := repo.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.Feature, want) {
		t.Errorf("Feature round-trip mismatch:\n got %+v\nwant %+v", got.Feature, want)
	}

	// All-nil optional fields stay NULL (FR-FE-2), not zero.
	saved, err = repo.Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Date(2026, 9, 26, 1, 16, 0, 0, time.UTC),
		Price: 2500, Feature: domain.Feature{VWAP: 1},
	})
	if err != nil {
		t.Fatalf("Insert empty: %v", err)
	}
	got, _ = repo.Get(ctx, saved.ID)
	if !reflect.DeepEqual(got.Feature, domain.Feature{VWAP: 1}) {
		t.Errorf("empty Feature round-trip = %+v, want only VWAP set", got.Feature)
	}
}

package featureengine_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// runBoard persists one snapshot built from the given bid/ask and returns it.
func runBoard(t *testing.T, symbol string, bid, ask float64) domain.Snapshot {
	t.Helper()
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, judgement.NewDecisionRepository(db), snapshots))
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), symbol)
	bidQty, askQty := 300.0, 100.0

	saved, err := engine.RunCycle(context.Background(), []featureengine.CycleInput{{
		InstrumentID: inst.ID,
		Symbol:       inst.Symbol,
		Input: featureengine.Input{
			Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
			Current:   featureengine.Reading{Price: 2409, VWAP: 2400, Bid: &bid, Ask: &ask, BidQty: &bidQty, AskQty: &askQty},
		},
	}})
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	return saved[0]
}

// A crossed book (bid > ask: special quote, around the open, or one stale
// side) must not produce a negative spread, which would pass the
// upper-bound-only spread guards (issue #465).
func TestEngine_RunCycle_CrossedBookLeavesSpreadAndMicropriceNil(t *testing.T) {
	snap := runBoard(t, "9433", 2410, 2409)
	if snap.SpreadBps != nil {
		t.Errorf("SpreadBps = %v, want nil for a crossed book (bid > ask)", *snap.SpreadBps)
	}
	if snap.Feature.Microprice != nil {
		t.Errorf("Microprice = %v, want nil for a crossed book", *snap.Feature.Microprice)
	}
}

func TestEngine_RunCycle_LockedBookHasZeroSpread(t *testing.T) {
	snap := runBoard(t, "9434", 2409, 2409)
	if snap.SpreadBps == nil || *snap.SpreadBps != 0 {
		t.Fatalf("SpreadBps = %v, want 0 for a locked book (bid == ask)", snap.SpreadBps)
	}
	if snap.Feature.Microprice == nil {
		t.Error("Microprice = nil, want a value for a locked book")
	}
}

func TestScreener_CrossedBookExcludedAsMissingSpread(t *testing.T) {
	snap := runBoard(t, "9435", 2410, 2409)
	cfg := config.FastScreenerConfig{
		MinPrice: 100, MaxPrice: 500000, MinTurnover5mJPY: 1, MaxSpreadBps: 50, TopN: 20,
	}
	in := screener.Input{InstrumentID: snap.InstrumentID, Symbol: snap.Symbol, Turnover5mJPY: 20_000_000, Snapshot: snap}

	reasons := screener.FilterReasons(cfg, in)
	if !reasons.Has(domain.ScreenReasonMissingSpread) {
		t.Errorf("reasons = %v, want missing_spread for a crossed book", reasons.List())
	}
	if screener.PassesFilter(cfg, in) {
		t.Error("crossed-book snapshot passed the Fast Screener, want excluded")
	}
}

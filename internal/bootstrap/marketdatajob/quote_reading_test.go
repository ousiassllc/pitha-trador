package marketdatajob

import (
	"context"
	"math"
	"strings"
	"testing"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func fptr(v float64) *float64 { return &v }

// readingFromQuote is a plain field-by-field mapping: broker.Quote already
// uses the conventional naming (Bid = best buy, Ask = best sell), so nothing
// is swapped here (the kabu swap lives in the adapter, kabu/quote; issue #458).
func TestReadingFromQuote_MapsFieldsWithoutSwapping(t *testing.T) {
	q := broker.Quote{
		Price: 2408, VWAP: 2400, Volume: 1000, Turnover: 2.4e6, High: fptr(2420), Low: fptr(2390),
		Bid: fptr(2407.5), BidQty: fptr(200), Ask: fptr(2408.5), AskQty: fptr(100),
		BidDepth: fptr(200), AskDepth: fptr(150), SpecialQuote: true,
	}

	r := readingFromQuote(q)

	if *r.Bid != 2407.5 || *r.Ask != 2408.5 {
		t.Errorf("Bid/Ask = %v/%v, want 2407.5/2408.5 (unswapped)", *r.Bid, *r.Ask)
	}
	if *r.BidQty != 200 || *r.AskQty != 100 {
		t.Errorf("BidQty/AskQty = %v/%v, want 200/100 (unswapped)", *r.BidQty, *r.AskQty)
	}
	if *r.BidDepth != 200 || *r.AskDepth != 150 {
		t.Errorf("BidDepth/AskDepth = %v/%v, want 200/150", *r.BidDepth, *r.AskDepth)
	}
	if r.Price != 2408 || r.VWAP != 2400 || r.Volume != 1000 || r.Turnover != 2.4e6 || *r.SessionHigh != 2420 || *r.SessionLow != 2390 || !r.SpecialQuote {
		t.Errorf("reading = %+v, scalar fields not carried over", r)
	}
}

func TestReadingFromQuote_MissingQuotesStayNil(t *testing.T) {
	r := readingFromQuote(broker.Quote{Price: 2408})
	if r.Bid != nil || r.Ask != nil || r.BidQty != nil || r.AskQty != nil || r.BidDepth != nil || r.AskDepth != nil {
		t.Errorf("reading = %+v, want all book fields nil", r)
	}
}

// The official sample must persist a positive spread and a buy-heavy
// (positive) orderbook imbalance, not the pre-fix negative values.
func TestHandleMarketData_SampleQuoteYieldsPositiveSpreadAndImbalance(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.quote = broker.Quote{
		Symbol: "7203", Price: 2408, VWAP: 2400, Volume: 1000000, Turnover: 2.4e9,
		Ask: fptr(2408.5), AskQty: fptr(100), // best sell
		Bid: fptr(2407.5), BidQty: fptr(200), // best buy
	}
	inst := mustCreateInstrument(t, env, "7203")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}

	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ListByInstrument = %d snaps, err %v; want 1", len(snaps), err)
	}
	s := snaps[0]
	if s.Bid == nil || *s.Bid != 2407.5 || s.Ask == nil || *s.Ask != 2408.5 {
		t.Fatalf("Bid/Ask = %v/%v, want 2407.5/2408.5", s.Bid, s.Ask)
	}
	wantSpread := 1.0 / 2408.0 * 10000
	if s.SpreadBps == nil || math.Abs(*s.SpreadBps-wantSpread) > 1e-6 {
		t.Errorf("SpreadBps = %v, want ~%.4f (positive)", s.SpreadBps, wantSpread)
	}
	wantImbalance := (200.0 - 100.0) / 300.0
	if s.Feature.OrderbookImbalance == nil || math.Abs(*s.Feature.OrderbookImbalance-wantImbalance) > 1e-9 {
		t.Errorf("OrderbookImbalance = %v, want %.4f (buy-heavy => positive)", s.Feature.OrderbookImbalance, wantImbalance)
	}
}

// A quote whose spread exceeds max_spread_bps must be excluded by Fast
// Screener (FR-FS-1) and exceed the Risk Engine's limit too, both of which
// read the persisted SpreadBps; before the fix it was always negative so
// neither guard could fire.
func TestHandleMarketData_WideSpreadBoardTripsSpreadGuards(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.quote = broker.Quote{
		Symbol: "7203", Price: 2500, VWAP: 2490, Volume: 1000000, Turnover: 2.49e9,
		Ask: fptr(2530), AskQty: fptr(100), // best sell
		Bid: fptr(2470), BidQty: fptr(100), // best buy
	}
	inst := mustCreateInstrument(t, env, "7203")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ListByInstrument = %d snaps, err %v; want 1", len(snaps), err)
	}

	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	riskCfg, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		t.Fatalf("LoadRiskBytes: %v", err)
	}
	spread := snaps[0].SpreadBps
	if spread == nil || *spread <= 0 {
		t.Fatalf("SpreadBps = %v, want > 0", spread)
	}

	reasons := screener.FilterReasons(strategy.FastScreener, screener.Input{Snapshot: snaps[0]})
	if !reasons.Has(domain.ScreenReasonMaxSpread) {
		t.Errorf("screen reasons = %v, want max_spread_bps (spread %.2f > %.2f)", reasons.List(), *spread, strategy.FastScreener.MaxSpreadBps)
	}

	riskEngine := risk.NewEngine(risk.Config{
		Limits:     riskCfg.Paper,
		KillSwitch: system.NewKillSwitchRepository(env.DB),
		Settings:   system.NewRuntimeSettingsRepository(env.DB),
		Snapshots:  env.Snapshots,
		Portfolio:  risk.ZeroPortfolioProvider{},
	})
	passed, reason := riskEngine.Check(context.Background(), inst.ID, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonMaxSpreadBps) {
		t.Errorf("risk Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxSpreadBps)
	}
}

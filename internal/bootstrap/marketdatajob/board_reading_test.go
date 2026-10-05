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
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func fptr(v float64) *float64 { return &v }

// kabuステーションAPI names quotes from the trader's side: BidPrice is the
// best SELL quote and AskPrice the best BUY quote (BoardSuccess sample:
// BidPrice 2408.5 > AskPrice 2407.5). readingFromBoard must swap them into
// the conventional Bid=buy / Ask=sell Reading (issue #458).
func TestReadingFromBoard_SwapsKabuSellBuyNaming(t *testing.T) {
	board := marketdata.Board{
		CurrentPrice: 2408,
		BidPrice:     fptr(2408.5), BidQty: fptr(100), // best sell
		AskPrice: fptr(2407.5), AskQty: fptr(200), // best buy
		Sell1: &marketdata.BoardLevel{Price: 2408.5, Qty: 100},
		Sell2: &marketdata.BoardLevel{Price: 2409, Qty: 50},
		Buy1:  &marketdata.BoardLevel{Price: 2407.5, Qty: 200},
	}

	r := readingFromBoard(board)

	if *r.Bid != 2407.5 || *r.Ask != 2408.5 {
		t.Errorf("Bid/Ask = %v/%v, want 2407.5/2408.5", *r.Bid, *r.Ask)
	}
	if *r.BidQty != 200 || *r.AskQty != 100 {
		t.Errorf("BidQty/AskQty = %v/%v, want 200/100", *r.BidQty, *r.AskQty)
	}
	if *r.BidDepth != 200 || *r.AskDepth != 150 {
		t.Errorf("BidDepth/AskDepth = %v/%v, want 200/150 (buy/sell levels)", *r.BidDepth, *r.AskDepth)
	}
}

func TestReadingFromBoard_MissingQuotesStayNil(t *testing.T) {
	r := readingFromBoard(marketdata.Board{CurrentPrice: 2408})
	if r.Bid != nil || r.Ask != nil || r.BidQty != nil || r.AskQty != nil || r.BidDepth != nil || r.AskDepth != nil {
		t.Errorf("reading = %+v, want all board fields nil", r)
	}
}

// The official sample must persist a positive spread and a buy-heavy
// (positive) orderbook imbalance, not the pre-fix negative values.
func TestHandleMarketData_KabuSampleBoardYieldsPositiveSpreadAndImbalance(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2408, VWAP: 2400, TradingVolume: 1000000, TradingValue: 2.4e9,
		BidPrice: fptr(2408.5), BidQty: fptr(100),
		AskPrice: fptr(2407.5), AskQty: fptr(200),
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

// A board whose spread exceeds max_spread_bps must be excluded by Fast
// Screener (FR-FS-1) and exceed the Risk Engine's limit too, both of which
// read the persisted SpreadBps; before the fix it was always negative so
// neither guard could fire.
func TestHandleMarketData_WideSpreadBoardTripsSpreadGuards(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9,
		BidPrice: fptr(2530), BidQty: fptr(100), // best sell
		AskPrice: fptr(2470), AskQty: fptr(100), // best buy
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

package fillmodel_test

import (
	"errors"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const (
	buy  = domain.OrderSideBuy
	sell = domain.OrderSideSell
)

func TestTickSize(t *testing.T) {
	tests := []struct{ price, want float64 }{
		{1, 1}, {3_000, 1}, {3_000.5, 5}, {5_000, 5}, {5_001, 10}, {30_000, 10}, {30_001, 50},
		{50_000, 50}, {50_001, 100}, {300_000, 100}, {300_001, 500}, {500_001, 1_000},
		{3_000_001, 5_000}, {60_000_000, 100_000},
	}
	for _, tt := range tests {
		if got := fillmodel.TickSize(tt.price); got != tt.want {
			t.Errorf("TickSize(%v) = %v, want %v", tt.price, got, tt.want)
		}
	}
}

func TestRounding(t *testing.T) {
	if got := fillmodel.RoundUp(2100); got != 2100 {
		t.Errorf("RoundUp(on-grid 2100) = %v, want unchanged", got)
	}
	if got := fillmodel.RoundUp(2100.2); got != 2101 {
		t.Errorf("RoundUp(2100.2) = %v, want 2101", got)
	}
	if got := fillmodel.RoundDown(2100.8); got != 2100 {
		t.Errorf("RoundDown(2100.8) = %v, want 2100", got)
	}
	if got := fillmodel.RoundUp(3_001); got != 3_005 {
		t.Errorf("RoundUp(3001) = %v, want 3005 (5円刻み)", got)
	}
	if got := fillmodel.RoundDown(3_004); got != 3_000 {
		t.Errorf("RoundDown(3004) = %v, want 3000", got)
	}
}

func TestMarket_ContinuousCrossesTheSpread(t *testing.T) {
	book := fillmodel.Book{Bid: 2099, Ask: 2101}
	m := fillmodel.Model{}
	if got, err := m.Market(buy, 2100, book, marketcalendar.PhaseContinuous); err != nil || got != 2101 {
		t.Errorf("Market(buy) = %v, %v; want the ask 2101", got, err)
	}
	if got, err := m.Market(sell, 2100, book, marketcalendar.PhaseContinuous); err != nil || got != 2099 {
		t.Errorf("Market(sell) = %v, %v; want the bid 2099", got, err)
	}
}

func TestMarket_SpreadBpsWithoutQuote(t *testing.T) {
	// 100bps spread around 2000: half = 50bps = 10 yen.
	book := fillmodel.Book{SpreadBps: 100}
	m := fillmodel.Model{}
	if got, _ := m.Market(buy, 2000, book, marketcalendar.PhaseContinuous); got != 2010 {
		t.Errorf("Market(buy) = %v, want 2010", got)
	}
	if got, _ := m.Market(sell, 2000, book, marketcalendar.PhaseContinuous); got != 1990 {
		t.Errorf("Market(sell) = %v, want 1990", got)
	}
}

func TestMarket_CrossedBookIsIgnored(t *testing.T) {
	book := fillmodel.Book{Bid: 2105, Ask: 2095}
	if got, _ := (fillmodel.Model{}).Market(buy, 2100, book, marketcalendar.PhaseContinuous); got != 2100 {
		t.Errorf("Market(buy, crossed book) = %v, want the last price 2100", got)
	}
}

func TestMarket_SlippageIsAdverseAndOnTheTickGrid(t *testing.T) {
	m := fillmodel.Model{SlippageBps: 10} // 0.10% of 2000 = 2 yen
	if got, _ := m.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseContinuous); got != 2002 {
		t.Errorf("Market(buy) = %v, want 2002", got)
	}
	if got, _ := m.Market(sell, 2000, fillmodel.Book{}, marketcalendar.PhaseContinuous); got != 1998 {
		t.Errorf("Market(sell) = %v, want 1998", got)
	}
	// 0.5 yen of slippage still costs a whole tick on a buy and nothing is
	// gained on a sell: rounding is against the order.
	m = fillmodel.Model{SlippageBps: 2.5}
	if got, _ := m.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseContinuous); got != 2001 {
		t.Errorf("Market(buy, sub-tick slippage) = %v, want 2001", got)
	}
	if got, _ := m.Market(sell, 2000, fillmodel.Book{}, marketcalendar.PhaseContinuous); got != 1999 {
		t.Errorf("Market(sell, sub-tick slippage) = %v, want 1999", got)
	}
}

func TestMarket_AuctionsPayNoSpreadButTheirOwnSlippage(t *testing.T) {
	m := fillmodel.Model{SlippageBps: 1, AuctionSlippageBps: 100} // 1% = 20 yen on 2000
	book := fillmodel.Book{Bid: 1990, Ask: 2010}
	for _, phase := range []marketcalendar.Phase{marketcalendar.PhaseOpeningAuction, marketcalendar.PhaseClosingAuction} {
		if got, err := m.Market(buy, 2000, book, phase); err != nil || got != 2020 {
			t.Errorf("Market(buy, %v) = %v, %v; want 2020 (indicative price + auction slippage, no spread)", phase, got, err)
		}
		if got, err := m.Market(sell, 2000, book, phase); err != nil || got != 1980 {
			t.Errorf("Market(sell, %v) = %v, %v; want 1980", phase, got, err)
		}
	}
}

func TestMarket_ClosedMarketNeverFills(t *testing.T) {
	_, err := fillmodel.Model{}.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseClosed)
	if !errors.Is(err, fillmodel.ErrNotTradable) {
		t.Fatalf("Market(closed) err = %v, want ErrNotTradable", err)
	}
	if _, ok := (fillmodel.Model{}).Limit(buy, 9999, 2000, fillmodel.Book{}, marketcalendar.PhaseClosed); ok {
		t.Fatal("Limit(closed) filled, want no fill")
	}
}

func TestLimit(t *testing.T) {
	book := fillmodel.Book{Bid: 2099, Ask: 2101}
	m := fillmodel.Model{SlippageBps: 10} // taker price 2101 -> 2103.1 -> 2104
	cont := marketcalendar.PhaseContinuous

	if _, ok := m.Limit(buy, 2100, 2100, book, cont); ok {
		t.Error("buy limit 2100 below the ask 2101 filled")
	}
	if p, ok := m.Limit(buy, 2101, 2100, book, cont); !ok || p != 2101 {
		t.Errorf("buy limit at the ask = %v, %v; want filled at the limit 2101 (never worse than the limit)", p, ok)
	}
	if p, ok := m.Limit(buy, 2200, 2100, book, cont); !ok || p != 2104 {
		t.Errorf("marketable buy limit = %v, %v; want the taker price 2104, not the limit", p, ok)
	}
	if _, ok := m.Limit(sell, 2100, 2100, book, cont); ok {
		t.Error("sell limit 2100 above the bid 2099 filled")
	}
	if p, ok := m.Limit(sell, 2099, 2100, book, cont); !ok || p != 2099 {
		t.Errorf("sell limit at the bid = %v, %v; want 2099", p, ok)
	}
}

func TestFeeAndSlippageBps(t *testing.T) {
	if got := (fillmodel.Model{FeeBps: 10}).Fee(2000, 100); math.Abs(got-200) > 1e-9 {
		t.Errorf("Fee = %v, want 200 (10bps of 200,000)", got)
	}
	if got := (fillmodel.Model{}).Fee(2000, 100); got != 0 {
		t.Errorf("zero-model Fee = %v, want 0", got)
	}
	if got := fillmodel.SlippageBps(buy, 2000, 2002); math.Abs(got-10) > 1e-9 {
		t.Errorf("SlippageBps(buy) = %v, want 10", got)
	}
	if got := fillmodel.SlippageBps(sell, 2000, 1998); math.Abs(got-10) > 1e-9 {
		t.Errorf("SlippageBps(sell) = %v, want 10", got)
	}
	if got := fillmodel.SlippageBps(buy, 2000, 1999); got >= 0 {
		t.Errorf("SlippageBps(buy, price improvement) = %v, want negative", got)
	}
}

func TestBookOf(t *testing.T) {
	bid, ask, spread := 2099.0, 2101.0, 9.5
	if got := fillmodel.BookOf(domain.Snapshot{Bid: &bid, Ask: &ask, SpreadBps: &spread}); got != (fillmodel.Book{Bid: 2099, Ask: 2101, SpreadBps: 9.5}) {
		t.Errorf("BookOf = %+v", got)
	}
	if got := fillmodel.BookOf(domain.Snapshot{}); got != (fillmodel.Book{}) {
		t.Errorf("BookOf(NULL quote) = %+v, want zero Book", got)
	}
}

package marketdatajob

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

type fakeSymbols struct {
	info marketdata.SymbolInfo
	err  error
}

func (f fakeSymbols) Get(context.Context, string) (marketdata.SymbolInfo, error) {
	return f.info, f.err
}

func bptr(v bool) *bool { return &v }

// Special quote, stop-high and lendable=false from the board / 銘柄情報
// reach the persisted snapshot (issue #511).
func TestHandleMarketData_PersistsTradabilityFlags(t *testing.T) {
	env := newTestEnv(t)
	env.Symbols = fakeSymbols{info: marketdata.SymbolInfo{MarginSell: bptr(false), UpperLimit: fptr(2500), LowerLimit: fptr(2000)}}
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9,
		BidPrice: fptr(2500.5), BidQty: fptr(100), AskPrice: fptr(2500), AskQty: fptr(100),
		BidSign: "0102",
	}
	inst := mustCreateInstrument(t, env, "7203")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 1)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ListByInstrument = %d snaps, err %v; want 1", len(snaps), err)
	}
	s := snaps[0]
	if !s.SpecialQuote || s.PriceLimit != domain.PriceLimitUp || s.Lendable == nil || *s.Lendable {
		t.Errorf("SpecialQuote/PriceLimit/Lendable = %v/%q/%v, want true/up/false", s.SpecialQuote, s.PriceLimit, s.Lendable)
	}
}

// A failed 銘柄情報 lookup must not stop the bar from being persisted; the
// flags stay "unknown" (no restriction).
func TestHandleMarketData_SymbolInfoFailureLeavesFlagsUnknown(t *testing.T) {
	env := newTestEnv(t)
	env.Symbols = fakeSymbols{err: errors.New("boom")}
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9,
	}
	inst := mustCreateInstrument(t, env, "7203")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 1)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ListByInstrument = %d snaps, err %v; want 1", len(snaps), err)
	}
	s := snaps[0]
	if s.SpecialQuote || s.PriceLimit != domain.PriceLimitNone || s.Lendable != nil {
		t.Errorf("SpecialQuote/PriceLimit/Lendable = %v/%q/%v, want false/none/nil", s.SpecialQuote, s.PriceLimit, s.Lendable)
	}
}

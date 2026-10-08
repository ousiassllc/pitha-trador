package marketdatajob

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

type fakeSymbols struct {
	info broker.SymbolInfo
	err  error
}

func (f fakeSymbols) Get(context.Context, string) (broker.SymbolInfo, error) {
	return f.info, f.err
}

func bptr(v bool) *bool { return &v }

// Special quote, stop-high and lendable=false from the board / 銘柄情報
// reach the persisted snapshot (issue #511).
func TestHandleMarketData_PersistsTradabilityFlags(t *testing.T) {
	env := newTestEnv(t)
	env.Symbols = fakeSymbols{info: broker.SymbolInfo{Lendable: bptr(false), UpperLimit: fptr(2500), LowerLimit: fptr(2000)}}
	env.Fake.quote = broker.Quote{
		Symbol: "7203", Price: 2500, VWAP: 2490, Volume: 1000000, Turnover: 2.49e9,
		Ask: fptr(2500.5), AskQty: fptr(100), Bid: fptr(2500), BidQty: fptr(100),
		SpecialQuote: true,
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
	env.Fake.quote = broker.Quote{
		Symbol: "7203", Price: 2500, VWAP: 2490, Volume: 1000000, Turnover: 2.49e9,
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

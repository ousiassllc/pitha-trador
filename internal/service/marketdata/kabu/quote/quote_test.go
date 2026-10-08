package quote_test

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/quote"
)

func fp(v float64) *float64 { return &v }

// kabuステーションAPI names quotes from the trader's side (issue #458): the
// board's BidPrice/BidQty is the best SELL quote and AskPrice/AskQty the best
// BUY quote. quote.FromBoard is the one place this is swapped; the neutral
// Quote uses the conventional naming.
func TestFromBoard_SwapsKabuBidAsk(t *testing.T) {
	board := marketdata.Board{
		Symbol: "7203", CurrentPrice: 2555, VWAP: 2550, TradingVolume: 1000, TradingValue: 2.5e6,
		BidPrice: fp(2556), BidQty: fp(300), // kabu "Bid" = best sell
		AskPrice: fp(2555), AskQty: fp(200), // kabu "Ask" = best buy
		HighPrice: fp(2600), LowPrice: fp(2500),
		Sell1: &marketdata.BoardLevel{Price: 2556, Qty: 300}, Sell2: &marketdata.BoardLevel{Price: 2557, Qty: 100},
		Buy1: &marketdata.BoardLevel{Price: 2555, Qty: 200},
	}
	q := quote.FromBoard(board)
	if q.Bid == nil || *q.Bid != 2555 || q.BidQty == nil || *q.BidQty != 200 {
		t.Errorf("Bid/BidQty = %v/%v, want the best BUY quote 2555/200", q.Bid, q.BidQty)
	}
	if q.Ask == nil || *q.Ask != 2556 || q.AskQty == nil || *q.AskQty != 300 {
		t.Errorf("Ask/AskQty = %v/%v, want the best SELL quote 2556/300", q.Ask, q.AskQty)
	}
	if q.BidDepth == nil || *q.BidDepth != 200 || q.AskDepth == nil || *q.AskDepth != 400 {
		t.Errorf("depth bid/ask = %v/%v, want Buy levels 200 / Sell levels 400", q.BidDepth, q.AskDepth)
	}
	if q.Symbol != "7203" || q.Price != 2555 || q.VWAP != 2550 || q.Volume != 1000 || q.Turnover != 2.5e6 || *q.High != 2600 || *q.Low != 2500 {
		t.Errorf("scalar fields not carried over: %+v", q)
	}
	if q.SpecialQuote {
		t.Error("SpecialQuote = true for a general quote")
	}
}

func TestFromBoard_SpecialQuoteAndMissingBook(t *testing.T) {
	q := quote.FromBoard(marketdata.Board{CurrentPrice: 100, BidSign: "0108"})
	if !q.SpecialQuote {
		t.Error("SpecialQuote = false for 停止前特別気配")
	}
	if q.Bid != nil || q.Ask != nil || q.BidDepth != nil || q.AskDepth != nil {
		t.Errorf("book fields = %+v, want nil when the board reports none", q)
	}
}

// The persisted raw_data_json is the broker's own response, unchanged by the
// neutralisation (docs/architecture/er/tables-market.md).
func TestFromBoard_RawIsTheBoardResponse(t *testing.T) {
	board := marketdata.Board{Symbol: "7203", CurrentPrice: 2555, BidPrice: fp(2556)}
	want, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(quote.FromBoard(board).Raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("Raw JSON = %s, want %s", got, want)
	}
}

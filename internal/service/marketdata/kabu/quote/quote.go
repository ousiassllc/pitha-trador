// Package quote translates a kabuステーションAPI board into the broker-neutral
// broker.Quote. It is the one place kabu's "Bid/Ask" naming is swapped to the
// conventional meaning (issue #458): kabuステーションAPI names quotes from the
// trader's side, so marketdata.Board.BidPrice/BidQty is the best SELL quote
// and AskPrice/AskQty the best BUY quote. It is a leaf package of the kabu
// adapter (internal/service/marketdata/kabu) so the PUSH feed can convert
// boards without importing the adapter.
package quote

import (
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// FromBoard converts b into a broker.Quote. Bid/BidQty is the best buy quote
// (the board's AskPrice/AskQty) and Ask/AskQty the best sell quote (the
// board's BidPrice/BidQty); the depth likewise maps Buy1..10 to BidDepth and
// Sell1..10 to AskDepth. Raw keeps the board itself, whose JSON is what the
// market-data job stores as raw_data_json.
func FromBoard(b marketdata.Board) broker.Quote {
	q := broker.Quote{
		Symbol:       b.Symbol,
		Price:        b.CurrentPrice,
		VWAP:         b.VWAP,
		Volume:       b.TradingVolume,
		Turnover:     b.TradingValue,
		High:         b.HighPrice,
		Low:          b.LowPrice,
		Bid:          b.AskPrice,
		BidQty:       b.AskQty,
		Ask:          b.BidPrice,
		AskQty:       b.BidQty,
		SpecialQuote: b.IsSpecialQuote(),
		Raw:          b,
	}
	if d, ok := b.BuyDepth(); ok {
		q.BidDepth = &d
	}
	if d, ok := b.SellDepth(); ok {
		q.AskDepth = &d
	}
	return q
}

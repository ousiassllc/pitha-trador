// Package quote translates a kabuステーション board into the conventional
// best bid/ask quote that the persisted 1-minute bar (market-data job) and
// the held-position monitor's transient snapshot must agree on. It lives
// beside internal/service/marketdata to keep that directory within the
// per-directory line budget.
package quote

import (
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// Bid is the best buy quote. kabuステーションAPI names quotes from the
// trader's side (marketdata.Board.BidPrice is the best SELL quote, AskPrice
// the best BUY quote), so the two are swapped here (issue #458). nil when
// the board does not report it.
func Bid(b marketdata.Board) *float64 { return b.AskPrice }

// Ask is the best sell quote (the board's BidPrice); nil when absent.
func Ask(b marketdata.Board) *float64 { return b.BidPrice }

// SpreadBps is the board's bid-ask spread in basis points of the mid price;
// nil when either quote is missing or the book is crossed
// (featureengine.SpreadBps).
func SpreadBps(b marketdata.Board) *float64 {
	return featureengine.SpreadBps(Bid(b), Ask(b))
}

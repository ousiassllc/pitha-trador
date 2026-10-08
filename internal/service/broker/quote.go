package broker

import (
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Quote is one symbol's 時価・板 snapshot in broker-neutral terms. Bid and Ask
// use the conventional meaning: Bid/BidQty is the best BUY quote and
// Ask/AskQty the best SELL quote, whatever the broker calls them (an adapter
// translates, e.g. kabuステーション names them from the trader's side, issue
// #458). Optional values are nil when the broker does not report them
// (FR-FE-2 欠損値扱い).
type Quote struct {
	Symbol string
	// Price is the current price; 0 while it is unresolved (before the
	// opening auction, no trade yet). See HasPrice.
	Price float64
	VWAP  float64
	// Volume and Turnover are cumulative for the session, not per bar.
	Volume   float64
	Turnover float64
	// High and Low are the session (当日) high/low.
	High *float64
	Low  *float64

	Bid    *float64
	BidQty *float64
	Ask    *float64
	AskQty *float64
	// BidDepth and AskDepth are the total quantity across every displayed
	// book level on the buy / sell side; nil when the book is not reported.
	BidDepth *float64
	AskDepth *float64
	// SpecialQuote is true while either side is a 特別気配 (a price-discovery
	// quote that does not trade at the displayed price).
	SpecialQuote bool

	// Raw is the broker's native response this Quote was built from. The
	// market-data job stores it JSON-encoded as market_snapshots.raw_data_json
	// (domain.Snapshot.RawDataJSON).
	Raw any
}

// HasPrice reports whether Price is a usable last price: anything not finite
// and positive means "missing", never a price of 0 (issue #173).
func (q Quote) HasPrice() bool {
	return q.Price > 0 && !math.IsInf(q.Price, 0)
}

// SymbolInfo is the subset of 銘柄情報 the entry-eligibility checks need
// (issue #511). The pointers are nil when the broker reports nothing (not a
// stock, e.g. an index).
type SymbolInfo struct {
	// Lendable is true for 貸借銘柄 (short selling is possible).
	Lendable *bool
	// UpperLimit and LowerLimit are the day's 値幅上限/値幅下限 (the stop-high /
	// stop-low prices).
	UpperLimit *float64
	LowerLimit *float64
}

// PriceLimit reports whether price sits at the day's upper limit
// (ストップ高) or lower limit (ストップ安), or domain.PriceLimitNone when it is
// neither or the limits are unknown.
func (s SymbolInfo) PriceLimit(price float64) domain.PriceLimit {
	switch {
	case s.UpperLimit != nil && price >= *s.UpperLimit:
		return domain.PriceLimitUp
	case s.LowerLimit != nil && price <= *s.LowerLimit:
		return domain.PriceLimitDown
	}
	return domain.PriceLimitNone
}

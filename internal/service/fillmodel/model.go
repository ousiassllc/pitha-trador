package fillmodel

import (
	"errors"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// ErrNotTradable is returned when nothing can fill: 昼休み, 立会時間外 or a
// non-trading day (marketcalendar.PhaseClosed).
var ErrNotTradable = errors.New("fillmodel: market is not open for execution")

// Book is the quote a fill is priced against. A zero field is unknown
// (market_snapshots stores NULL when the 板 is unavailable, FR-FE-2).
type Book struct {
	Bid, Ask  float64
	SpreadBps float64
}

// BookOf is snap's quote; NULL columns stay zero.
func BookOf(snap domain.Snapshot) Book {
	var b Book
	if snap.Bid != nil {
		b.Bid = *snap.Bid
	}
	if snap.Ask != nil {
		b.Ask = *snap.Ask
	}
	if snap.SpreadBps != nil {
		b.SpreadBps = *snap.SpreadBps
	}
	return b
}

// Model is the cost assumption. The zero value charges no slippage and no
// fee (it still respects the tick grid, the spread and the session).
type Model struct {
	// SlippageBps worsens a ザラ場 fill beyond the touch.
	SlippageBps float64
	// AuctionSlippageBps worsens a 寄り/引け fill versus the indicative price.
	AuctionSlippageBps float64
	// FeeBps is charged on each fill's notional.
	FeeBps float64
}

// Default is the assumption every production path uses. Fees are 0: the
// 証券会社 behind kabuステーションAPI (三菱UFJ eスマート証券) charges nothing
// for 国内株式現物 since 2026-05-18. The spread is modelled explicitly, so
// SlippageBps is only the depth/latency cost beyond the touch.
func Default() Model {
	return Model{SlippageBps: 2, AuctionSlippageBps: 5, FeeBps: 0}
}

// touch is the price a market order of side meets before slippage.
func touch(side string, ref float64, book Book, phase marketcalendar.Phase) float64 {
	if phase != marketcalendar.PhaseContinuous {
		return ref // 板寄せ: one price, no spread to cross
	}
	buy := side == domain.OrderSideBuy
	if book.Bid > 0 && book.Ask > 0 && book.Bid <= book.Ask {
		if buy {
			return book.Ask
		}
		return book.Bid
	}
	if book.SpreadBps > 0 {
		half := book.SpreadBps / 2 / 10_000
		if buy {
			return ref * (1 + half)
		}
		return ref * (1 - half)
	}
	return ref
}

// Market is the price a market order of side fills at, given the last
// price ref and its quote. It fails with ErrNotTradable when the market
// is closed.
func (m Model) Market(side string, ref float64, book Book, phase marketcalendar.Phase) (float64, error) {
	if phase == marketcalendar.PhaseClosed {
		return 0, ErrNotTradable
	}
	slip := m.SlippageBps
	if phase != marketcalendar.PhaseContinuous {
		slip = m.AuctionSlippageBps
	}
	p := touch(side, ref, book, phase)
	if side == domain.OrderSideBuy {
		return RoundUp(p * (1 + slip/10_000)), nil
	}
	return RoundDown(p * (1 - slip/10_000)), nil
}

// Limit is the price a limit order of side at limit fills at, and
// whether it fills at all: only when the touch satisfies the limit, and
// never worse than the limit (a marketable limit order still takes the
// touch). Nothing fills while the market is closed.
func (m Model) Limit(side string, limit, ref float64, book Book, phase marketcalendar.Phase) (float64, bool) {
	if phase == marketcalendar.PhaseClosed {
		return 0, false
	}
	p, err := m.Market(side, ref, book, phase)
	if err != nil {
		return 0, false
	}
	t := touch(side, ref, book, phase)
	if side == domain.OrderSideBuy {
		return math.Min(limit, p), t <= limit
	}
	return math.Max(limit, p), t >= limit
}

// Fee is the commission for filling qty shares at price.
func (m Model) Fee(price float64, qty int64) float64 {
	return price * float64(qty) * m.FeeBps / 10_000
}

// SlippageBps is how many basis points worse than the reference price ref
// a fill at price was for side (negative: price improvement). It covers
// the tick rounding, the spread and the slippage together.
func SlippageBps(side string, ref, price float64) float64 {
	if side == domain.OrderSideBuy {
		return (price - ref) / ref * 10_000
	}
	return (ref - price) / ref * 10_000
}

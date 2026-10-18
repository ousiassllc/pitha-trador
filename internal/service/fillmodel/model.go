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

// Model is the cost assumption of one trade direction. The zero value charges
// no slippage and no fee (it still respects the tick grid, the spread and the
// session). Costs holds one Model per direction.
type Model struct {
	// SlippageBps worsens a ザラ場 fill beyond the touch.
	SlippageBps float64
	// OpeningSlippageBps and ClosingSlippageBps worsen a 寄り / 引け fill
	// versus the indicative price.
	OpeningSlippageBps float64
	ClosingSlippageBps float64
	// FeeBps is charged on each fill's notional.
	FeeBps float64
	// FeeFixedJPY is charged on each fill on top of FeeBps.
	FeeFixedJPY float64
	// ReferenceNotionalJPY is the order size FeeFixedJPY is converted to bps
	// at where a fill has no size (the backtest's percentage returns). Zero
	// converts the fixed fee to 0 bps.
	ReferenceNotionalJPY float64
	// ShortBorrowBps is the 貸株料 charged once per SHORT position as bps of
	// its entry notional (Model.BorrowFee); 0 for a LONG model.
	ShortBorrowBps float64
}

// slippage is the slippage of a fill in phase (a closed market is rejected
// before this).
func (m Model) slippage(phase marketcalendar.Phase) float64 {
	switch phase {
	case marketcalendar.PhaseOpeningAuction:
		return m.OpeningSlippageBps
	case marketcalendar.PhaseClosingAuction:
		return m.ClosingSlippageBps
	}
	return m.SlippageBps
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
	slip := m.slippage(phase)
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

// Fee is the commission for filling qty shares at price: FeeBps of the
// notional plus FeeFixedJPY.
func (m Model) Fee(price float64, qty int64) float64 {
	return price*float64(qty)*m.FeeBps/10_000 + m.FeeFixedJPY
}

// BorrowFee is the 貸株料 of a SHORT position of qty shares entered at
// entryPrice.
func (m Model) BorrowFee(entryPrice float64, qty int64) float64 {
	return entryPrice * float64(qty) * m.ShortBorrowBps / 10_000
}

// FeeBpsPerFill is the commission of one fill as bps of the notional, the
// fixed part converted at ReferenceNotionalJPY.
func (m Model) FeeBpsPerFill() float64 {
	if m.ReferenceNotionalJPY <= 0 {
		return m.FeeBps
	}
	return m.FeeBps + m.FeeFixedJPY/m.ReferenceNotionalJPY*10_000
}

// RoundTripFeeBps is the explicit fee cost of one round trip as bps of the
// entry price: the commission of both fills plus the 貸株料. The slippage is
// not part of it: it is already in the fill prices.
func (m Model) RoundTripFeeBps() float64 {
	return 2*m.FeeBpsPerFill() + m.ShortBorrowBps
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

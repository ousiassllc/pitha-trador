package marketcalendar

import "time"

// Phase is the kind of execution a timestamp falls into on the TSE.
// Paper Trading and the Backtest Engine fill orders differently in each
// (internal/service/fillmodel): 寄り/引けの気配は板寄せの単一約定で、
// ザラ場の1分足とは別の約定として扱う。
type Phase int

const (
	// PhaseClosed is every time nothing executes: non-trading days, before
	// 9:00, the 11:30-12:30 lunch break and from 15:30 on.
	PhaseClosed Phase = iota
	// PhaseOpeningAuction is the first minute of each session (9:00 前場
	// 寄り, 12:30 後場寄り), when the 寄り付き板寄せ crosses the opening
	// quotes at a single price.
	PhaseOpeningAuction
	// PhaseContinuous is ザラ場: the rest of each session, except the
	// closing auction.
	PhaseContinuous
	// PhaseClosingAuction is the 大引け クロージング・オークション
	// (15:25-15:30). 前場 has no closing auction: it ends in ザラ場.
	PhaseClosingAuction
)

// String is the phase's name for logs and test failure messages.
func (p Phase) String() string {
	switch p {
	case PhaseOpeningAuction:
		return "opening_auction"
	case PhaseContinuous:
		return "continuous"
	case PhaseClosingAuction:
		return "closing_auction"
	default:
		return "closed"
	}
}

const (
	// openingAuctionMinutes is how long after each session open the bar is
	// still the 寄り (the 1-minute bar stamped 9:00 / 12:30).
	openingAuctionMinutes = 1
	// closingAuctionStart is when 大引けのクロージング・オークション begins
	// (ザラ場 ends at 15:25, the call runs until the 15:30 close).
	closingAuctionStart = 15*60 + 25
)

// PhaseAt classifies t. It is PhaseClosed wherever IsOpen is false, and
// otherwise one of the three execution phases above.
func (c Calendar) PhaseAt(t time.Time) Phase {
	if !c.IsOpen(t) {
		return PhaseClosed
	}
	t = t.In(JST)
	m := t.Hour()*60 + t.Minute()
	switch {
	case m >= closingAuctionStart:
		return PhaseClosingAuction
	case m < morningOpen+openingAuctionMinutes, m >= afternoonOpen && m < afternoonOpen+openingAuctionMinutes:
		return PhaseOpeningAuction
	default:
		return PhaseContinuous
	}
}

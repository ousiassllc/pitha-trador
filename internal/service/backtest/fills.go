package backtest

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Sessions classifies a bar's timestamp into how an order at that moment
// executes (marketcalendar.Calendar implements it). A nil Sessions treats
// every bar as ザラ場, which is what synthetic test series without real
// 立会時間 timestamps need.
type Sessions interface {
	PhaseAt(t time.Time) marketcalendar.Phase
}

// executor fills the replay's orders exactly as Paper Trading does
// (internal/service/execution, same fillmodel): the bar's last price plus
// its quote become a fill with spread, slippage, tick grid and fee instead
// of the bar price itself, 昼休み・立会時間外 bars never fill, and 寄り/引け
// bars fill as the 板寄せ rather than as a 1-minute ザラ場 bar.
type executor struct {
	cost     fillmodel.Costs
	sessions Sessions
}

func (x executor) phase(bar domain.Snapshot) marketcalendar.Phase {
	if x.sessions == nil {
		return marketcalendar.PhaseContinuous
	}
	return x.sessions.PhaseAt(bar.Timestamp)
}

// fill is the price a market order of side gets on bar for a position of
// direction (the cost settings price LONG and SHORT separately); ok is false
// when the bar is not tradable (昼休み・立会時間外).
func (x executor) fill(bar domain.Snapshot, direction, side string) (price float64, ok bool) {
	price, err := x.cost.For(direction).Market(side, bar.Price, fillmodel.BookOf(bar), x.phase(bar))
	return price, err == nil
}

// entrySide/exitSide are the order sides opening/closing a position of
// direction.
func entrySide(direction string) string {
	if direction == domain.JevDirectionShort {
		return domain.OrderSideSell
	}
	return domain.OrderSideBuy
}

func exitSide(direction string) string {
	if direction == domain.JevDirectionShort {
		return domain.OrderSideBuy
	}
	return domain.OrderSideSell
}

// closeTimes is what a Sessions that knows the 大引け time (marketcalendar.
// Calendar, execution.MarketCalendar) adds: the force-flat-before-close exit
// and the end of the entry session need it. A Sessions without it (nil, or a
// test stub) has neither.
type closeTimes interface {
	CloseAt(t time.Time) (time.Time, bool)
}

// closeAt is the 大引け time of t's trading day, or false when there is no
// calendar or t is on a non-trading day.
func (x executor) closeAt(t time.Time) (time.Time, bool) {
	if c, ok := x.sessions.(closeTimes); ok {
		return c.CloseAt(t)
	}
	return time.Time{}, false
}

// newTrade builds the Trade of a position opened on entry (filled at
// entryFill) and closed on exitBar (filled at exitFill) with reason. The
// returns are direction-adjusted percentages: gross between the two bars' last
// prices, slippage between the two fills, net after the fill model's fees on
// both fills (and a SHORT's 貸株料).
func newTrade(entry, exitBar domain.Snapshot, direction string, entryFill, exitFill float64, reason string, roundTripFeeBps float64) Trade {
	sign := 1.0
	if direction == domain.JevDirectionShort {
		sign = -1.0
	}
	grossReturnPct := sign * (exitBar.Price/entry.Price - 1) * 100
	// Execution costs (spread, slippage, tick grid, 寄り/引けの気配): the
	// return between the two actual fills, before fees.
	slippageReturnPct := sign * (exitFill/entryFill - 1) * 100

	// Commission on both fills (a fixed fee at the reference notional) plus
	// the SHORT 貸株料, as % of the entry price.
	feeDeductionPct := roundTripFeeBps / 100
	return Trade{
		InstrumentID:      entry.InstrumentID,
		Symbol:            entry.Symbol,
		Direction:         direction,
		EntryTimestamp:    entry.Timestamp,
		EntryPrice:        entryFill,
		ExitTimestamp:     exitBar.Timestamp,
		ExitPrice:         exitFill,
		ExitReason:        reason,
		GrossReturnPct:    grossReturnPct,
		SlippageReturnPct: slippageReturnPct,
		FeeReturnPct:      grossReturnPct - feeDeductionPct,
		NetReturnPct:      slippageReturnPct - feeDeductionPct,
	}
}

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
	cost     fillmodel.Model
	sessions Sessions
}

func (x executor) phase(bar domain.Snapshot) marketcalendar.Phase {
	if x.sessions == nil {
		return marketcalendar.PhaseContinuous
	}
	return x.sessions.PhaseAt(bar.Timestamp)
}

// fill is the price a market order of side gets on bar; ok is false when
// the bar is not tradable (昼休み・立会時間外).
func (x executor) fill(bar domain.Snapshot, side string) (price float64, ok bool) {
	price, err := x.cost.Market(side, bar.Price, fillmodel.BookOf(bar), x.phase(bar))
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

// closeTrade walks forward from bars[entryIdx+1:] applying exit's Stop
// Loss/Take Profit/max holding conditions (in that priority order at
// each bar) and returns the resulting Trade plus the index of the bar it
// exited on, so replay's caller resumes scanning for the next entry
// immediately after it. entryFill is the entry's fill price (what a paper
// position's EntryPrice would be); the exit fills on its bar via x. Bars
// x cannot fill on (昼休み・立会時間外) are walked past without evaluating
// exit conditions, as live Paper Exit cannot fill there either.
func closeTrade(bars []domain.Snapshot, entryIdx int, direction string, entryFill float64, exit ExitRule, x executor, instrumentID int64, symbol string) (Trade, int) {
	entry := bars[entryIdx]
	sign := 1.0
	if direction == domain.JevDirectionShort {
		sign = -1.0
	}

	exitIdx := entryIdx
	exitFill := entryFill
	reason := ExitReasonDataEnded
	for j := entryIdx + 1; j < len(bars); j++ {
		if exit.MaxHolding > 0 && bars[j].Timestamp.Sub(entry.Timestamp) > exit.MaxHolding {
			reason = ExitReasonMaxHolding
			break
		}
		fill, ok := x.fill(bars[j], exitSide(direction))
		if !ok {
			continue
		}
		exitIdx, exitFill = j, fill

		retPct := sign * (bars[j].Price/entryFill - 1) * 100
		if exit.StopLossPct > 0 && retPct <= -exit.StopLossPct {
			reason = ExitReasonStopLoss
			break
		}
		if exit.TakeProfitPct > 0 && retPct >= exit.TakeProfitPct {
			reason = ExitReasonTakeProfit
			break
		}
	}

	exitBar := bars[exitIdx]
	grossReturnPct := sign * (exitBar.Price/entry.Price - 1) * 100
	// Execution costs (spread, slippage, tick grid, 寄り/引けの気配): the
	// return between the two actual fills, before fees.
	slippageReturnPct := sign * (exitFill/entryFill - 1) * 100

	feeDeductionPct := x.cost.FeeBps / 100 * 2
	feeReturnPct := grossReturnPct - feeDeductionPct
	netReturnPct := slippageReturnPct - feeDeductionPct

	return Trade{
		InstrumentID:      instrumentID,
		Symbol:            symbol,
		Direction:         direction,
		EntryTimestamp:    entry.Timestamp,
		EntryPrice:        entryFill,
		ExitTimestamp:     exitBar.Timestamp,
		ExitPrice:         exitFill,
		ExitReason:        reason,
		GrossReturnPct:    grossReturnPct,
		SlippageReturnPct: slippageReturnPct,
		FeeReturnPct:      feeReturnPct,
		NetReturnPct:      netReturnPct,
	}, exitIdx
}

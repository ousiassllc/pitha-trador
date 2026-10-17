// Package exitcond is FR-EXIT-1's exit-condition evaluation as a pure
// function: the one priority order both Paper Trading
// (internal/service/execution.Engine.EvaluateExit) and the backtest replay
// (internal/service/backtest) evaluate an open position with, so the two
// cannot drift apart (issue #775). It lives in its own directory, next to
// internal/service/execution, to keep that directory within the
// per-directory line budget (like vwapcross).
package exitcond

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

// Params are FR-EXIT-2's numeric exit settings. A zero value disables that
// one condition (the force-flat condition is disabled by Market.MarketCloseAt
// being nil instead: a zero ForceFlatBeforeClose means "at the close").
type Params struct {
	StopLossPct     float64
	TakeProfitPct   float64
	TrailingStopPct float64
	MaxHolding      time.Duration
	// ForceFlatBeforeClose is how long before the close a position is
	// force-closed (FR-EXIT-1's 引け前強制決済).
	ForceFlatBeforeClose time.Duration
}

// Market is one evaluation's view of the position's instrument.
type Market struct {
	Price float64
	// VWAP is nil when unavailable, which disables VWAP逆クロス.
	VWAP *float64
	// PrevVWAP is the price/VWAP seen at the position's previous evaluation,
	// the baseline for VWAP逆クロス; nil on the first evaluation, when the
	// entry price against the current VWAP is used instead.
	PrevVWAP *vwapcross.Observation
	// Decision is the latest Jev Trader decision, or nil when Jev is
	// unavailable (FR-EXIT-3), which disables only the two Jev-derived
	// conditions (Jev方向反転, continuation_probability低下).
	Decision *domain.JevDecision
	// MarketCloseAt is today's 大引け time; nil disables 引け前強制決済.
	MarketCloseAt *time.Time
	Now           time.Time
}

// Lookups are the conditions' inputs that cost more than the Market's
// values. Both are called lazily, only when the conditions before them did
// not already decide the exit, and a nil function disables its condition's
// lookup (see each field).
type Lookups struct {
	// BestPrice returns the best price the position has reached since it was
	// opened (the peak for a LONG, the trough for a SHORT), the baseline of
	// Trailing Stop. nil disables Trailing Stop (no price history to read).
	BestPrice func() (float64, error)
	// ContinuationDropped reports whether continuation_probability (read from
	// the Decision) is low enough to exit. nil means the plain limit given by
	// the caller is not applied either: continuation_probability低下 is
	// disabled.
	ContinuationDropped func(continuationProbability float64) bool
}

// Evaluate implements FR-EXIT-1's eight exit conditions in a fixed priority
// order: fixed Stop Loss, fixed Take Profit, Trailing Stop, Jev方向反転,
// continuation_probability低下, VWAP逆クロス, max holding time, then 引け前
// 強制決済. It returns the first triggered condition's domain.ExitReason*
// and true, or ("", false) if none has triggered yet. An error comes only
// from lk.BestPrice.
//
// FR-EXIT-3: every condition except the two Jev-derived ones keeps working
// from position/mkt alone, so a Jev API outage never stops Exit management.
func Evaluate(p Params, position domain.Position, mkt Market, lk Lookups) (string, bool, error) {
	sign := Sign(position.Side)
	retPct := sign * (mkt.Price/position.EntryPrice - 1) * 100

	if p.StopLossPct > 0 && retPct <= -p.StopLossPct {
		return domain.ExitReasonStopLoss, true, nil
	}
	if p.TakeProfitPct > 0 && retPct >= p.TakeProfitPct {
		return domain.ExitReasonTakeProfit, true, nil
	}

	if p.TrailingStopPct > 0 && lk.BestPrice != nil {
		best, err := lk.BestPrice()
		if err != nil {
			return "", false, err
		}
		if TrailingTriggered(p.TrailingStopPct, sign, best, mkt.Price) {
			return domain.ExitReasonTrailingStop, true, nil
		}
	}

	if mkt.Decision != nil {
		if Reversed(position.Side, mkt.Decision.Direction) {
			return domain.ExitReasonJevDirectionReversed, true, nil
		}
		if mkt.Decision.ContinuationProbability != nil && lk.ContinuationDropped != nil && lk.ContinuationDropped(*mkt.Decision.ContinuationProbability) {
			return domain.ExitReasonContinuationProbDrop, true, nil
		}
	}

	if mkt.VWAP != nil {
		prevPrice, prevVWAP := position.EntryPrice, *mkt.VWAP // no previous evaluation: entry price vs current VWAP
		if mkt.PrevVWAP != nil {
			prevPrice, prevVWAP = mkt.PrevVWAP.Price, mkt.PrevVWAP.VWAP
		}
		if vwapcross.Crossed(position.Side, prevPrice, prevVWAP, mkt.Price, *mkt.VWAP) {
			return domain.ExitReasonVWAPCross, true, nil
		}
	}

	if p.MaxHolding > 0 && mkt.Now.Sub(position.OpenedAt) >= p.MaxHolding {
		return domain.ExitReasonMaxHolding, true, nil
	}

	if mkt.MarketCloseAt != nil {
		forceFlatAt := mkt.MarketCloseAt.Add(-p.ForceFlatBeforeClose)
		if !mkt.Now.Before(forceFlatAt) {
			return domain.ExitReasonForceFlatBeforeClose, true, nil
		}
	}

	return "", false, nil
}

// Sign returns +1 for a LONG position (profits as price rises) or -1 for a
// SHORT one.
func Sign(side string) float64 {
	if side == domain.PositionSideShort {
		return -1
	}
	return 1
}

// TrailingTriggered reports whether price has retraced trailingStopPct from
// best (the peak for a LONG, the trough for a SHORT).
func TrailingTriggered(trailingStopPct, sign, best, price float64) bool {
	if sign*price > sign*best {
		best = price
	}
	return sign*(best-price)/best*100 >= trailingStopPct
}

// Reversed reports whether jevDirection is the opposite of positionSide (a
// LONG position with a SHORT Jev direction, or vice versa) - Jev方向反転.
func Reversed(positionSide string, jevDirection *string) bool {
	if jevDirection == nil {
		return false
	}
	switch positionSide {
	case domain.PositionSideLong:
		return *jevDirection == domain.JevDirectionShort
	case domain.PositionSideShort:
		return *jevDirection == domain.JevDirectionLong
	default:
		return false
	}
}

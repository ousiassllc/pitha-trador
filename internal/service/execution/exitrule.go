package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// MarketContext is EvaluateExit's per-tick input: the position's
// instrument's latest price/VWAP, the latest Jev Trader decision (if
// any), and today's market-close time (for the force-flat-before-close
// condition).
type MarketContext struct {
	Price float64
	// VWAP is nil when unavailable, disabling the VWAP逆クロス condition
	// (mirrors Decision's FR-EXIT-3 nil-safety).
	VWAP *float64
	// Decision is the latest Jev Trader decision for this instrument, or
	// nil when Jev is unavailable (FR-EXIT-3: "Jev API不応答時も...継続
	// 動作"). Its Direction/ContinuationProbability drive the Jev方向反
	// 転/continuation_probability低下 conditions; a caller reading a
	// decision back from repository.DecisionRepository must first pass it
	// through EnrichDecision (decision.go) to populate
	// ContinuationProbability, which is not one of that repository's
	// queryable columns.
	Decision *domain.JevDecision
	// MarketCloseAt is today's market close time, or nil to disable the
	// 引け前強制決済 condition (no trading-calendar concept exists yet
	// to derive it from).
	MarketCloseAt *time.Time
	// Now defaults to time.Now() when zero. Tests set it explicitly for
	// deterministic max-holding/force-flat-before-close checks.
	Now time.Time
}

func (mkt MarketContext) now() time.Time {
	if mkt.Now.IsZero() {
		return time.Now().UTC()
	}
	return mkt.Now
}

// EvaluateExit implements FR-EXIT-1's eight exit conditions, evaluated
// together in a fixed priority order (matching
// internal/service/backtest's closeTrade precedent for the subset it
// shares): fixed Stop Loss, fixed Take Profit, Trailing Stop, Jev方向反
// 転, continuation_probability低下, VWAP逆クロス, max holding time, then
// 引け前強制決済. It returns the first triggered condition's
// domain.ExitReason* and true, or ("", false) if none has triggered yet.
//
// FR-EXIT-3: every condition except the two Jev-derived ones
// (mkt.Decision == nil disables just those two) keeps working from
// position/mkt alone, so a Jev API outage never stops Exit management.
func (e *Engine) EvaluateExit(ctx context.Context, position domain.Position, mkt MarketContext) (string, bool, error) {
	sign := positionSign(position.Side)
	retPct := sign * (mkt.Price/position.EntryPrice - 1) * 100
	now := mkt.now()

	if e.cfg.StopLossPct > 0 && retPct <= -e.cfg.StopLossPct {
		return domain.ExitReasonStopLoss, true, nil
	}
	if e.cfg.TakeProfitPct > 0 && retPct >= e.cfg.TakeProfitPct {
		return domain.ExitReasonTakeProfit, true, nil
	}

	if e.cfg.TrailingStopPct > 0 {
		triggered, err := e.trailingStopTriggered(ctx, position, mkt.Price, sign, now)
		if err != nil {
			return "", false, err
		}
		if triggered {
			return domain.ExitReasonTrailingStop, true, nil
		}
	}

	if mkt.Decision != nil {
		if reversed(position.Side, mkt.Decision.Direction) {
			return domain.ExitReasonJevDirectionReversed, true, nil
		}
		if mkt.Decision.ContinuationProbability != nil && *mkt.Decision.ContinuationProbability < e.cfg.MinContinuationProbability {
			return domain.ExitReasonContinuationProbDrop, true, nil
		}
	}

	if mkt.VWAP != nil && vwapCrossedAgainst(position.Side, mkt.Price, *mkt.VWAP) {
		return domain.ExitReasonVWAPCross, true, nil
	}

	if e.cfg.MaxHoldingMinutes > 0 {
		maxHolding := time.Duration(e.cfg.MaxHoldingMinutes) * time.Minute
		if now.Sub(position.OpenedAt) >= maxHolding {
			return domain.ExitReasonMaxHolding, true, nil
		}
	}

	if mkt.MarketCloseAt != nil {
		forceFlatAt := mkt.MarketCloseAt.Add(-time.Duration(e.cfg.ForceFlatBeforeMarketCloseMinutes) * time.Minute)
		if !now.Before(forceFlatAt) {
			return domain.ExitReasonForceFlatBeforeClose, true, nil
		}
	}

	return "", false, nil
}

// trailingStopTriggered reports whether position's price has retraced
// TrailingStopPct from the best price reached since OpenedAt (the peak
// for a LONG, the trough for a SHORT), derived from
// repository.SnapshotRepository.ListByInstrumentRange rather than a
// dedicated positions column (doc.go). It returns false without error
// when Engine has no SnapshotRepository configured (Deps.Snapshots was
// left nil).
func (e *Engine) trailingStopTriggered(ctx context.Context, position domain.Position, currentPrice, sign float64, now time.Time) (bool, error) {
	if e.snapshots == nil {
		return false, nil
	}
	history, err := e.snapshots.ListByInstrumentRange(ctx, position.InstrumentID, position.OpenedAt, now)
	if err != nil {
		return false, fmt.Errorf("execution: trailing stop lookback for position %d: %w", position.ID, err)
	}

	best := position.EntryPrice
	for _, snap := range history {
		if sign*snap.Price > sign*best {
			best = snap.Price
		}
	}
	if sign*currentPrice > sign*best {
		best = currentPrice
	}

	retracedPct := sign * (best - currentPrice) / best * 100
	return retracedPct >= e.cfg.TrailingStopPct, nil
}

// reversed reports whether jevDirection is the opposite of positionSide
// (a LONG position with a SHORT Jev direction, or vice versa) - Jev方向
// 反転.
func reversed(positionSide string, jevDirection *string) bool {
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

// vwapCrossedAgainst reports whether price is now on the side of vwap
// that works against positionSide (a LONG position with price below
// VWAP, or a SHORT position with price above it) - VWAP逆クロス.
func vwapCrossedAgainst(positionSide string, price, vwap float64) bool {
	if positionSide == domain.PositionSideLong {
		return price < vwap
	}
	return price > vwap
}

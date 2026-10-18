package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/exitcond"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
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
	// decision back from judgement.DecisionRepository must first pass it
	// through enrich.Decision to populate
	// ContinuationProbability, which is not one of that repository's
	// queryable columns.
	Decision *domain.JevDecision
	// MarketCloseAt is today's 大引け time (OnSnapshot: Config.Calendar);
	// nil disables the 引け前強制決済 condition.
	MarketCloseAt *time.Time
	// PrevVWAP is the price/VWAP observed at this position's previous
	// evaluation, the baseline for VWAP逆クロス (a cross, not a stay on
	// the adverse side). nil on the first evaluation (e.g. after a
	// restart): EntryPrice vs the current VWAP is used instead.
	PrevVWAP *vwapcross.Observation
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
// together in the fixed priority order of exitcond.Evaluate (shared with the
// backtest replay): fixed Stop Loss, fixed Take Profit, Trailing Stop, Jev方向
// 反転, continuation_probability低下, VWAP逆クロス, max holding time, then 引け
// 前強制決済. It returns the first triggered condition's domain.ExitReason*
// and true, or ("", false) if none has triggered yet.
//
// FR-EXIT-3: every condition except the two Jev-derived ones
// (mkt.Decision == nil disables just those two) keeps working from
// position/mkt alone, so a Jev API outage never stops Exit management.
func (e *Engine) EvaluateExit(ctx context.Context, position domain.Position, mkt MarketContext) (string, bool, error) {
	now := mkt.now()
	params := exitcond.Params{
		StopLossPct:          e.cfg.StopLossPct,
		TakeProfitPct:        e.cfg.TakeProfitPct,
		TrailingStopPct:      e.cfg.TrailingStopPct,
		MaxHolding:           time.Duration(e.cfg.MaxHoldingMinutes) * time.Minute,
		ForceFlatBeforeClose: time.Duration(e.cfg.ForceFlatBeforeMarketCloseMinutes) * time.Minute,
	}
	lookups := exitcond.Lookups{
		ContinuationDropped: func(p float64) bool { return e.continuationProbDrop(ctx, position.Side, p) },
	}
	if e.snapshots != nil {
		lookups.BestPrice = func() (float64, error) { return e.bestPriceSinceOpen(ctx, position, mkt.Price, now) }
	}
	return exitcond.Evaluate(params, position, exitcond.Market{
		Price: mkt.Price, VWAP: mkt.VWAP, PrevVWAP: mkt.PrevVWAP, Decision: mkt.Decision,
		MarketCloseAt: mkt.MarketCloseAt, Now: now,
	}, lookups)
}

// bestPriceSinceOpen is Trailing Stop's baseline: the best price position
// has reached since OpenedAt (the peak for a LONG, the trough for a SHORT),
// derived from market.SnapshotRepository.ListByInstrumentRange rather than a
// dedicated positions column (doc.go). Callers only use it when Engine has a
// SnapshotRepository configured (Deps.Snapshots).
func (e *Engine) bestPriceSinceOpen(ctx context.Context, position domain.Position, currentPrice float64, now time.Time) (float64, error) {
	history, err := e.snapshots.ListByInstrumentRange(ctx, position.InstrumentID, position.OpenedAt, now)
	if err != nil {
		return 0, fmt.Errorf("execution: trailing stop lookback for position %d: %w", position.ID, err)
	}

	sign := exitcond.Sign(position.Side)
	best := position.EntryPrice
	for _, snap := range history {
		if sign*snap.Price > sign*best {
			best = snap.Price
		}
	}
	if sign*currentPrice > sign*best {
		best = currentPrice
	}
	return best, nil
}

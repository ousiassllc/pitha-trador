package backtest

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/exitcond"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

// ExitRule is the replay's FR-EXIT-1/2 exit setting, evaluated by
// exitcond.Evaluate - the same function, with the same priority order, Paper
// Trading evaluates its open positions with (issue #775): fixed Stop Loss,
// fixed Take Profit, Trailing Stop, Jev方向反転, continuation_probability低下,
// VWAP逆クロス, max holding time, 引け前強制決済. A zero-valued numeric field
// disables that condition (as in execution.Config); 引け前強制決済 applies
// whenever Sessions knows the 大引け time. A caller passes execution.Config's
// values (internal/bootstrap/backtestsource does).
type ExitRule struct {
	StopLossPct     float64
	TakeProfitPct   float64
	TrailingStopPct float64
	MaxHolding      time.Duration
	// MinContinuationProbability is the upper bound of the
	// continuation_probability低下 threshold: the exit fires below
	// min(this, the entry threshold of the position's side in
	// RunConfig.Thresholds), as execution's does.
	MinContinuationProbability float64
	// ForceFlatBeforeClose is how long before the 大引け a position is
	// force-closed, and new entries stop.
	ForceFlatBeforeClose time.Duration
	// CooldownAfterLoss is the per-symbol re-entry pause after a losing close
	// (execution.Config.CooldownAfterLossMinutes).
	CooldownAfterLoss time.Duration
}

func (r ExitRule) params() exitcond.Params {
	return exitcond.Params{
		StopLossPct:          r.StopLossPct,
		TakeProfitPct:        r.TakeProfitPct,
		TrailingStopPct:      r.TrailingStopPct,
		MaxHolding:           r.MaxHolding,
		ForceFlatBeforeClose: r.ForceFlatBeforeClose,
	}
}

// openTrade is a replayed position: what exit evaluation needs besides the
// bars.
type openTrade struct {
	direction string
	entryBar  domain.Snapshot
	entryFill float64
	quantity  int64
	regime    string
	abArm     string
	// predictedEVBps is the model's EV at the entry decision (Trade.PredictedEVBps).
	predictedEVBps *float64

	// best is the best bar price since the entry (Trailing Stop's baseline),
	// prev the price/VWAP observed at the last evaluation that did not exit
	// (VWAP逆クロス's baseline, nil before the first one).
	best float64
	prev *vwapcross.Observation
	// lastFillIdx/lastFill are the latest bar after the entry that could
	// fill an exit, and its fill price: where a position still open when the
	// data ends is closed (ExitReasonDataEnded). lastFillIdx is the entry
	// index while there is none.
	lastFillIdx int
	lastFill    float64
}

// evaluateExit asks exitcond.Evaluate whether the position of st is to be
// closed on bars[idx]; the Jev decision is the latest one recorded at or
// before the bar (nil when none: FR-EXIT-3).
func (st *instState) evaluateExit(ctx context.Context, idx int) (string, bool, error) {
	bar := st.bars[idx]
	op := st.open
	d, ok, err := st.cfg.Decisions.Decision(ctx, st.cfg.InstrumentID, bar.Timestamp)
	if err != nil {
		return "", false, err
	}
	mkt := exitcond.Market{Price: bar.Price, Now: bar.Timestamp}
	if ok {
		mkt.Decision = &d
	}
	if closeAt, ok := st.x.closeAt(bar.Timestamp); ok {
		mkt.MarketCloseAt = &closeAt
	}
	if bar.Feature.VWAP > 0 {
		vwap := bar.Feature.VWAP
		mkt.VWAP, mkt.PrevVWAP = &vwap, op.prev
	}
	side := domain.PositionSideLong
	if op.direction == domain.JevDirectionShort {
		side = domain.PositionSideShort
	}
	position := domain.Position{InstrumentID: st.cfg.InstrumentID, Side: side, EntryPrice: op.entryFill, OpenedAt: op.entryBar.Timestamp}
	return exitcond.Evaluate(st.cfg.Exit.params(), position, mkt, exitcond.Lookups{
		BestPrice:           func() (float64, error) { return op.best, nil },
		ContinuationDropped: func(p float64) bool { return p < st.continuationLimit(side) },
	})
}

// continuationLimit is the continuation_probability below which a position of
// side exits: min(ExitRule's limit, the side's entry threshold), as live.
func (st *instState) continuationLimit(side string) float64 {
	entry := st.cfg.Thresholds.Policy.Short.MinContinuationProbability
	if side == domain.PositionSideLong {
		entry = st.cfg.Thresholds.Policy.Long.MinContinuationProbability
	}
	return min(st.cfg.Exit.MinContinuationProbability, entry)
}

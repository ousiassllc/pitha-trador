package calibration

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// bpsPerUnit converts a fractional price return to basis points.
const bpsPerUnit = 10000.0

// barrierPoint is one market_snapshots bar of a labeling path, as a
// direction-adjusted gross return (before trading costs) from the entry
// reference price, in bps. Favorable and Adverse are the best and worst
// excursions inside the bar and Close its closing level. A snapshot bar
// carries one price, so the labeler sets all three equal; they are separate
// so that a bar touching both barriers resolves conservatively (see
// firstBarrier) if finer-grained bars are ever fed in.
type barrierPoint struct {
	Favorable float64
	Adverse   float64
	Close     float64
}

// barrierOutcome is the triple-barrier verdict of one labeling path.
type barrierOutcome struct {
	// Hit is domain.BarrierHitTakeProfit, BarrierHitStopLoss or
	// BarrierHitTimeout.
	Hit string
	// Index is the position in the path of the bar that ended the label
	// (the last bar for a timeout).
	Index int
	// NetReturnBps is the direction-adjusted return after the assumed
	// trading cost: a take-profit exits at +TakeProfitBps, a stop-loss at
	// -StopLossBps (the barrier levels, as in the triple-barrier method),
	// and a timeout at the last bar's close less the cost.
	NetReturnBps float64
}

// firstBarrier walks points in time order and returns the first barrier
// touched. The barriers are cost-inclusive: the take-profit is touched when
// the gross return less b.CostBps reaches b.TakeProfitBps, the stop-loss
// when it falls to -b.StopLossBps. A bar touching both is a stop-loss (the
// pessimistic order, since the intra-bar sequence is unknown). When neither
// is touched the label times out on the last point, graded by its net
// return rather than as "flat = wrong". points must not be empty.
func firstBarrier(points []barrierPoint, b barriersettings.Barriers) barrierOutcome {
	for i, p := range points {
		switch {
		case p.Adverse-b.CostBps <= -b.StopLossBps:
			return barrierOutcome{Hit: domain.BarrierHitStopLoss, Index: i, NetReturnBps: -b.StopLossBps}
		case p.Favorable-b.CostBps >= b.TakeProfitBps:
			return barrierOutcome{Hit: domain.BarrierHitTakeProfit, Index: i, NetReturnBps: b.TakeProfitBps}
		}
	}
	last := len(points) - 1
	return barrierOutcome{Hit: domain.BarrierHitTimeout, Index: last, NetReturnBps: points[last].Close - b.CostBps}
}

// grossReturnBps is the direction-adjusted return from entry to price in
// bps; factor is +1 for LONG and -1 for SHORT.
func grossReturnBps(entry, price, factor float64) float64 {
	return (price - entry) / entry * bpsPerUnit * factor
}

// EntryWaitTolerance is how long after the Jev response the entry bar may
// land: the first bar at or after the response is the entry, and a gap
// longer than this (a missed cycle plus jitter, lunch break, close) means
// there is no executable entry reference for the decision.
const EntryWaitTolerance = 3 * time.Minute

// TimeoutHorizons returns the horizon resolver for
// scheduler.WithOutcomeLabelHorizons: the one triple-barrier timeout in
// Settings (issue #760), read from store on every outcome-labeling scan so a
// change applies without a restart.
func TimeoutHorizons(store barriersettings.Store) func(ctx context.Context) ([]int, error) {
	return func(ctx context.Context) ([]int, error) {
		barriers, err := barriersettings.Load(ctx, store)
		if err != nil {
			return nil, err
		}
		return []int{barriers.TimeoutMinutes()}, nil
	}
}

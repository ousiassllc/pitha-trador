package exitalign

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/exitcond"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

// NotEvaluatedExits are the exit conditions Replayer cannot evaluate from
// the stored data, as domain.ExitReason* values.
var NotEvaluatedExits = []string{
	domain.ExitReasonJevDirectionReversed,
	domain.ExitReasonContinuationProbDrop,
	domain.ExitReasonForceFlatBeforeClose,
}

// grossReturnBps is the direction-adjusted return from entry to price in bps;
// factor is +1 for LONG and -1 for SHORT.
func grossReturnBps(entry, price, factor float64) float64 {
	return (price - entry) / entry * 10000 * factor
}

// replayedExit is one decision replayed under an ExitConfig.
type replayedExit struct {
	// NetBps is the direction-adjusted return at the exit bar after the label
	// cost.
	NetBps float64
	// Reason is the domain.ExitReason* that ended the replay.
	Reason string
}

// replayExit replays a position entered at entryRef on entryBar through path
// (the bars after it, ascending) under rule, bar by bar with
// exitcond.Evaluate - the one evaluation Paper Trading and the backtest replay
// use (issue #775), so the priority order cannot drift: fixed stop-loss, fixed
// take-profit, trailing stop, VWAP逆クロス, maximum holding time. The
// conditions market_snapshots has no data for (Jev方向反転,
// continuation_probability低下, 引け前強制決済: NotEvaluatedExits) are disabled
// by passing no decision and no close time. The exit is at the triggering
// bar's price (gross), less costBps. ok=false when no condition triggered and
// the bars end more than calibration.HorizonBarTolerance before the maximum
// holding time (the data does not cover the rule).
func replayExit(entryBar domain.Snapshot, path []domain.Snapshot, entryRef float64, direction string, rule ExitConfig, costBps float64) (replayedExit, bool) {
	factor := 1.0
	side := domain.PositionSideLong
	if direction == domain.JevDirectionShort {
		factor, side = -1.0, domain.PositionSideShort
	}
	params := exitcond.Params{
		StopLossPct: rule.StopLossPct, TakeProfitPct: rule.TakeProfitPct,
		TrailingStopPct: rule.TrailingStopPct, MaxHolding: rule.maxHolding(),
	}
	position := domain.Position{Side: side, EntryPrice: entryRef, OpenedAt: entryBar.Timestamp}
	best := entryRef
	var prev *vwapcross.Observation
	if entryBar.Feature.VWAP > 0 {
		prev = &vwapcross.Observation{Price: entryBar.Price, VWAP: entryBar.Feature.VWAP}
	}
	exitAt := func(bar domain.Snapshot, reason string) (replayedExit, bool) {
		return replayedExit{NetBps: grossReturnBps(entryRef, bar.Price, factor) - costBps, Reason: reason}, true
	}
	for _, bar := range path {
		if factor*bar.Price > factor*best {
			best = bar.Price
		}
		mkt := exitcond.Market{Price: bar.Price, Now: bar.Timestamp}
		if bar.Feature.VWAP > 0 {
			vwap := bar.Feature.VWAP
			mkt.VWAP, mkt.PrevVWAP = &vwap, prev
		}
		reason, exit, err := exitcond.Evaluate(params, position, mkt, exitcond.Lookups{
			BestPrice: func() (float64, error) { return best, nil },
		})
		if err != nil { // BestPrice never fails
			return replayedExit{}, false
		}
		if exit {
			return exitAt(bar, reason)
		}
		if bar.Feature.VWAP > 0 {
			prev = &vwapcross.Observation{Price: bar.Price, VWAP: bar.Feature.VWAP}
		}
	}
	if rule.maxHolding() > 0 && len(path) > 0 && path[len(path)-1].Timestamp.After(entryBar.Timestamp.Add(rule.maxHolding()-calibration.HorizonBarTolerance)) {
		return exitAt(path[len(path)-1], domain.ExitReasonMaxHolding)
	}
	return replayedExit{}, false
}

// ClosedReturnSource lists the realized returns of the positions closed in a
// window (*trading.PositionRepository implements it).
type ClosedReturnSource interface {
	ListClosedReturns(ctx context.Context, from, to time.Time) ([]trading.ClosedReturn, error)
}

// DecisionReader loads a Jev decision (*judgement.DecisionRepository).
type DecisionReader interface {
	Get(ctx context.Context, id int64) (domain.JevDecision, error)
}

// SnapshotReader lists a instrument's market_snapshots in [from, to) without
// the stored board (*market.SnapshotRepository).
type SnapshotReader interface {
	ListHistoryByInstrumentRange(ctx context.Context, instrumentID int64, from, to time.Time) ([]domain.Snapshot, error)
}

// OutcomeReader streams the calibration_outcomes of the decisions in a window
// (*calibration.CalibrationRepository).
type OutcomeReader interface {
	EachOutcomeByDecisionRange(ctx context.Context, from, to time.Time, fn func(domain.CalibrationOutcome) error) error
}

// Replayer compares the triple-barrier labels with the same decisions
// replayed under the actual exit rules (issue #774). It reads
// calibration_outcomes, jev_decisions and market_snapshots and stores nothing.
type Replayer struct {
	decisions DecisionReader
	snapshots SnapshotReader
	outcomes  OutcomeReader
	settings  Settings
	positions ClosedReturnSource
}

// NewReplayer returns a Replayer reading the triple-barrier and cost
// settings from settings (the label cost is the one Labeler applies) and, when
// positions is non-nil, the paper exit reasons of the compared window.
func NewReplayer(decisions DecisionReader, snapshots SnapshotReader, outcomes OutcomeReader, settings Settings, positions ClosedReturnSource) *Replayer {
	return &Replayer{decisions: decisions, snapshots: snapshots, outcomes: outcomes, settings: settings, positions: positions}
}

// replayGrace is how far past the maximum holding time bars are read, so a bar
// landing on it is included.
const replayGrace = time.Second

// Compare replays every triple-barrier label of the decisions timestamped in
// [from, to) whose horizon is the current Settings timeout.
func (r *Replayer) Compare(ctx context.Context, from, to time.Time, rule ExitConfig) (LabelExitComparison, error) {
	barriers, err := barriersettings.Load(ctx, r.settings)
	if err != nil {
		return LabelExitComparison{}, fmt.Errorf("calibration: load triple-barrier settings: %w", err)
	}
	costs, err := costsettings.Load(ctx, r.settings)
	if err != nil {
		return LabelExitComparison{}, fmt.Errorf("calibration: load cost settings: %w", err)
	}
	horizon := barriers.TimeoutMinutes()
	out := LabelExitComparison{
		From: from, To: to, HorizonMinutes: horizon, NotEvaluatedExits: append([]string(nil), NotEvaluatedExits...),
		CostLongBps:  costs.LabelCostBps(barriers.CostBps, domain.JevDirectionLong),
		CostShortBps: costs.LabelCostBps(barriers.CostBps, domain.JevDirectionShort),
	}

	// Collect first: the cursor must not be held open while the per-decision
	// queries below run.
	var labels []domain.CalibrationOutcome
	err = r.outcomes.EachOutcomeByDecisionRange(ctx, from, to, func(o domain.CalibrationOutcome) error {
		if o.LabelVersion == domain.CalibrationLabelVersionTripleBarrier && o.HorizonMinutes == horizon &&
			o.BarrierHit != "" && o.NetReturnBps != nil && o.EntryRefPrice != nil {
			labels = append(labels, o)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("calibration: read triple-barrier labels: %w", err)
	}

	var labelNet, replayNet, diff []float64
	reasons := map[string]int{}
	for _, label := range labels {
		replay, ok, err := r.replay(ctx, label, rule, out.CostLongBps, out.CostShortBps)
		if err != nil {
			return out, err
		}
		if !ok {
			out.SkippedCount++
			continue
		}
		labelNet = append(labelNet, *label.NetReturnBps)
		replayNet = append(replayNet, replay.NetBps)
		diff = append(diff, replay.NetBps-*label.NetReturnBps)
		reasons[replay.Reason]++
		switch label.BarrierHit {
		case domain.BarrierHitTakeProfit:
			out.LabelTPCount++
		case domain.BarrierHitStopLoss:
			out.LabelSLCount++
		default:
			out.LabelTimeoutCount++
		}
	}
	out.ReplayedCount = len(diff)
	out.LabelNetBps, out.ReplayNetBps, out.DiffBps = summarize(labelNet), summarize(replayNet), summarize(diff)
	out.ReplayExits = reasonCounts(reasons)

	if r.positions != nil {
		closed, err := r.positions.ListClosedReturns(ctx, from, to)
		if err != nil {
			return out, fmt.Errorf("calibration: read closed paper positions: %w", err)
		}
		paper := map[string]int{}
		for _, c := range closed {
			paper[c.ExitReason]++
		}
		out.PaperExits = reasonCounts(paper)
	}
	return out, nil
}

// replay replays one labeled decision. ok=false when the stored bars do not
// give an entry bar or do not cover the rule (the decision is skipped).
func (r *Replayer) replay(ctx context.Context, label domain.CalibrationOutcome, rule ExitConfig, costLong, costShort float64) (replayedExit, bool, error) {
	decision, err := r.decisions.Get(ctx, label.JevDecisionID)
	if err != nil {
		return replayedExit{}, false, fmt.Errorf("calibration: load jev decision %d: %w", label.JevDecisionID, err)
	}
	direction := domain.JevDirectionNone
	if decision.Direction != nil {
		direction = *decision.Direction
	}
	if direction != domain.JevDirectionLong && direction != domain.JevDirectionShort {
		return replayedExit{}, false, nil
	}
	cost := costLong
	if direction == domain.JevDirectionShort {
		cost = costShort
	}
	responseAt := calibration.ResponseTime(decision)
	window, err := r.snapshots.ListHistoryByInstrumentRange(ctx, decision.InstrumentID, responseAt, responseAt.Add(calibration.EntryWaitTolerance+rule.maxHolding()+replayGrace))
	if err != nil {
		return replayedExit{}, false, fmt.Errorf("calibration: load market snapshots for decision %d: %w", label.JevDecisionID, err)
	}
	entryIdx, entryRef := calibration.FindEntry(window, direction, responseAt)
	if entryIdx < 0 {
		return replayedExit{}, false, nil
	}
	entry := window[entryIdx]
	path := window[entryIdx+1:]
	for len(path) > 0 && rule.maxHolding() > 0 && path[len(path)-1].Timestamp.Sub(entry.Timestamp) > rule.maxHolding() {
		path = path[:len(path)-1]
	}
	replay, ok := replayExit(entry, path, entryRef, direction, rule, cost)
	return replay, ok, nil
}

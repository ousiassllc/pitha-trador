package calibration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// DefaultHorizonsMinutes are the judgment horizons Outcome Labeling
// evaluates each Jev trader decision at (functional.md §4.12,
// docs/architecture/er.md §calibration_outcomes "horizon_minutes: 5/10/15
// 等"): short holds of a few minutes up to about 15 minutes, matching the
// small-edge-stacking style the strategy is judged on (issue #711).
var DefaultHorizonsMinutes = []int{5, 10, 15}

// HorizonBarTolerance is how far before decision.Timestamp+horizon the
// last market_snapshots bar of a labeling window may fall (two 1-minute
// bars: one missed cycle plus timestamp jitter). A window that ends
// earlier - because the horizon crosses the lunch break (11:30-12:30),
// the close (15:30), or a data gap - is not labeled (FR-CAL-4), so a
// shortened horizon is never recorded as the full 5/10/15-minute outcome.
const HorizonBarTolerance = 2 * time.Minute

// horizonDataGrace is how long after decision timestamp + horizon the
// labeling window may still be filling in (market-data cycle lag, a slow
// job queue) before a window that falls short of the horizon is treated as
// permanently unlabelable: bars that have not arrived by then (lunch break,
// close, outage) never will, so the pair is recorded as skipped instead of
// being retried (issue #481).
const horizonDataGrace = 5 * time.Minute

// horizonRetryInterval is how long a deferred (not yet labelable) job waits
// before its next attempt: one 1-minute market_snapshots bar.
const horizonRetryInterval = time.Minute

// Labeler turns one Jev trader decision (jev_decisions,
// decision_type=trader) into a calibration_outcomes row for one judgment
// horizon (functional.md FR-CAL-4) by the triple-barrier method (issue #762,
// parent #751): the first of take-profit, stop-loss and timeout touched on
// the market_snapshots bars after the Jev response decides the label, with
// cost-inclusive barrier widths from Settings (barriersettings).
type Labeler struct {
	decisions *judgement.DecisionRepository
	snapshots *market.SnapshotRepository
	outcomes  *calrepo.CalibrationRepository
	barriers  barriersettings.Store
	costs     costsettings.Store // nil: the label cost is the barrier cost alone
	now       func() time.Time
}

// Option configures a Labeler.
type Option func(*Labeler)

// WithClock overrides the clock HandleJob uses to tell "data not landed
// yet" from "data will never land" (default time.Now), for tests.
func WithClock(now func() time.Time) Option {
	return func(l *Labeler) { l.now = now }
}

// WithBarrierSettings makes HandleJob read the TP/SL/cost widths from the
// runtime settings in store on every job, so a Settings change applies to
// the next label without a restart. Without it the barriersettings defaults
// apply.
func WithBarrierSettings(store barriersettings.Store) Option {
	return func(l *Labeler) { l.barriers = store }
}

// WithCostSettings moves the label cost by the Settings cost settings (#779),
// so the label - and the meta-labeling EV trained on it - assumes what Paper
// fills charge. See costsettings.Settings.LabelCostBps.
func WithCostSettings(store costsettings.Store) Option {
	return func(l *Labeler) { l.costs = store }
}

// NewLabeler returns a Labeler that loads decisions via decisions, market
// data via snapshots, and persists calibration_outcomes rows via outcomes.
func NewLabeler(decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository, outcomes *calrepo.CalibrationRepository, opts ...Option) *Labeler {
	l := &Labeler{decisions: decisions, snapshots: snapshots, outcomes: outcomes, now: time.Now}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// loadBarriers returns the effective barrier widths.
func (l *Labeler) loadBarriers(ctx context.Context) (barriersettings.Barriers, error) {
	if l.barriers == nil {
		return barriersettings.Default(), nil
	}
	return barriersettings.Load(ctx, l.barriers)
}

// HandleJob processes one outcome-labeling queue job
// (calrepo.OutcomeLabelJobPayload): it loads the identified jev_decisions
// row and the market_snapshots bars after the Jev response, and persists the
// triple-barrier calibration_outcomes row (FR-CAL-4). Its signature matches
// internal/service/scheduler.Handler (JobQueueOutcomeLabeling's handler).
// The label is LabelWindow's (entry quote, path, barriers).
//
// If the bars do not (yet) cover the path, HandleJob never records a shortened
// window and never fails the job (issue #710, #481): see shortWindow.
func (l *Labeler) HandleJob(ctx context.Context, job jobqueue.Job) error {
	var payload calrepo.OutcomeLabelJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("calibration: decode outcome-labeling job payload: %w", err)
	}
	decision, outcome, gap, err := l.Compute(ctx, payload.JevDecisionID, payload.HorizonMinutes)
	if err != nil {
		return err
	}
	if gap != "" {
		return l.shortWindow(ctx, decision, payload.HorizonMinutes, ResponseTime(decision), gap)
	}
	if _, err := l.outcomes.Insert(ctx, outcome); err != nil {
		return fmt.Errorf("calibration: persist outcome for decision %d (horizon %dm): %w", payload.JevDecisionID, payload.HorizonMinutes, err)
	}
	return nil
}

// Compute is HandleJob's label without its persistence, shared with the
// counterfactual label of a skipped candidate (issue #1052): the decision and
// its outcome, or the non-empty reason the bars do not cover the path.
func (l *Labeler) Compute(ctx context.Context, jevDecisionID int64, horizonMinutes int) (decision domain.JevDecision, outcome domain.CalibrationOutcome, gap string, err error) {
	decision, err = l.decisions.Get(ctx, jevDecisionID)
	if err != nil {
		return decision, outcome, "", fmt.Errorf("calibration: load jev decision %d: %w", jevDecisionID, err)
	}
	barriers, err := l.loadBarriers(ctx)
	if err != nil {
		return decision, outcome, "", fmt.Errorf("calibration: load triple-barrier settings: %w", err)
	}

	direction := domain.JevDirectionNone
	if decision.Direction != nil {
		direction = *decision.Direction
	}
	if l.costs != nil {
		if barriers, err = costsettings.LabelBarriers(ctx, l.costs, barriers, direction); err != nil {
			return decision, outcome, "", fmt.Errorf("calibration: load cost settings: %w", err)
		}
	}
	// The label cost the settings imply must still leave a valid barrier set
	// (cost below the stop-loss): fail the job rather than write a label whose
	// stop-loss sits on the profit side.
	if err := barriers.Validate(); err != nil {
		return decision, outcome, "", fmt.Errorf("calibration: effective triple-barrier settings are invalid: %w", err)
	}
	responseAt := ResponseTime(decision)
	timeout := time.Duration(horizonMinutes) * time.Minute
	// +1s: ListByInstrumentRange's `to` is exclusive; a bar exactly on the
	// latest timeout boundary must stay in.
	window, err := l.snapshots.ListByInstrumentRange(ctx, decision.InstrumentID, responseAt, responseAt.Add(EntryWaitTolerance+timeout+time.Second))
	if err != nil {
		return decision, outcome, "", fmt.Errorf("calibration: load market snapshots for decision %d: %w", jevDecisionID, err)
	}

	if outcome, gap = LabelWindow(window, responseAt, direction, horizonMinutes, barriers); gap == "" {
		outcome.JevDecisionID = decision.ID
	}
	return decision, outcome, gap, nil
}

// LabelWindow is the pure label of one decision from window, the bars of its
// instrument from responseAt (ascending, as ListByInstrumentRange returns
// them): the outcome (no decision id), or the non-empty reason the window does
// not cover the path. The entry is the first bar within EntryWaitTolerance of
// responseAt with an executable quote (ask for LONG, bid for SHORT); the path
// runs horizonMinutes from it. A take-profit is touched when the gross return
// less the cost reaches the TP width, a stop-loss when it falls to the SL
// width; the first decides the label, neither is a timeout graded by its net
// return. NONE has no side to price: the raw path, no barrier fields. barriers
// must be the effective (cost-moved, valid) ones. Nothing is read or written.
func LabelWindow(window []domain.Snapshot, responseAt time.Time, direction string, horizonMinutes int, barriers barriersettings.Barriers) (domain.CalibrationOutcome, string) {
	entryIdx, entryRef := FindEntry(window, direction, responseAt)
	if entryIdx < 0 {
		return domain.CalibrationOutcome{}, "no bar with an executable entry quote within " + EntryWaitTolerance.String() + " of the Jev response"
	}
	deadline := window[entryIdx].Timestamp.Add(time.Duration(horizonMinutes) * time.Minute)
	path := window[entryIdx+1:]
	for len(path) > 0 && path[len(path)-1].Timestamp.After(deadline) {
		path = path[:len(path)-1]
	}
	if len(path) == 0 || path[len(path)-1].Timestamp.Before(deadline.Add(-HorizonBarTolerance)) {
		reason := "no bars after the entry bar"
		if len(path) > 0 {
			reason = fmt.Sprintf("market data ends at %s, short of the timeout (lunch break, close, or data gap)", path[len(path)-1].Timestamp.Format(time.RFC3339))
		}
		return domain.CalibrationOutcome{}, reason
	}
	outcome := labelPath(path, entryRef, direction, barriers)
	outcome.HorizonMinutes = horizonMinutes
	return outcome, ""
}

// labelPath computes the calibration_outcomes row (without its identity) for
// path, the bars after the entry bar through the timeout, measured from
// entryRef. Price excursions (FutureReturn, MAE, MFE) run to the bar that
// ended the label.
func labelPath(path []domain.Snapshot, entryRef float64, direction string, barriers barriersettings.Barriers) domain.CalibrationOutcome {
	factor := 1.0
	if direction == domain.JevDirectionShort {
		factor = -1.0
	}
	points := make([]barrierPoint, len(path))
	for i, bar := range path {
		gross := grossReturnBps(entryRef, bar.Price, factor)
		points[i] = barrierPoint{Favorable: gross, Adverse: gross, Close: gross}
	}

	end := len(path) - 1
	var hit barrierOutcome
	if direction != domain.JevDirectionNone {
		hit = firstBarrier(points, barriers)
		end = hit.Index
	}

	mfe, mae := 0.0, 0.0
	for _, bar := range path[:end+1] {
		adjusted := percentReturn(entryRef, bar.Price) * factor
		mfe = max(mfe, adjusted)
		mae = min(mae, adjusted)
	}
	outcome := domain.CalibrationOutcome{
		FutureReturn:          percentReturn(entryRef, path[end].Price),
		MaxAdverseExcursion:   mae,
		MaxFavorableExcursion: mfe,
		LabelVersion:          domain.CalibrationLabelVersionTripleBarrier,
	}
	if direction == domain.JevDirectionNone {
		return outcome
	}

	// Correct iff the trade made money after costs: a take-profit always
	// does, a stop-loss never, a timeout by the sign of its net return.
	correct := hit.NetReturnBps > 0
	outcome.WasDirectionCorrect = &correct
	outcome.BarrierHit = hit.Hit
	outcome.NetReturnBps = &hit.NetReturnBps
	outcome.EntryRefPrice = &entryRef
	return outcome
}

// shortWindow handles a window that does not reach the horizon, never failing
// the job (issue #710): within horizonDataGrace of the Jev response +
// EntryWaitTolerance + horizon the data may still land (jobqueue.Defer, retried);
// after it the gap is permanent: the skip marker is recorded (jobqueue.Skip).
// No calibration_outcomes row is written either way (FR-CAL-4).
func (l *Labeler) shortWindow(ctx context.Context, decision domain.JevDecision, horizonMinutes int, responseAt time.Time, reason string) error {
	horizon := time.Duration(horizonMinutes) * time.Minute
	now := l.now()
	if now.Before(responseAt.Add(EntryWaitTolerance + horizon + horizonDataGrace)) {
		return jobqueue.Defer(now.Add(horizonRetryInterval),
			fmt.Sprintf("decision %d horizon %dm not labelable yet: %s", decision.ID, horizonMinutes, reason))
	}
	if err := l.outcomes.MarkUnlabelable(ctx, decision.ID, horizonMinutes, reason); err != nil {
		return fmt.Errorf("calibration: %w", err)
	}
	slog.Info("calibration: decision horizon permanently unlabelable, skipping (no shortened-horizon outcome recorded)",
		"jev_decision_id", decision.ID, "horizon_minutes", horizonMinutes, "reason", reason)
	return jobqueue.Skip(fmt.Sprintf("decision %d horizon %dm permanently unlabelable: %s", decision.ID, horizonMinutes, reason))
}

// percentReturn is the % price return from entry to price (1.0 == +1%, as in
// internal/service/backtest.Trade).
func percentReturn(entry, price float64) float64 {
	return (price - entry) / entry * 100
}

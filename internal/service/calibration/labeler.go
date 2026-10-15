package calibration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
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

// horizonBarTolerance is how far before decision.Timestamp+horizon the
// last market_snapshots bar of a labeling window may fall (two 1-minute
// bars: one missed cycle plus timestamp jitter). A window that ends
// earlier - because the horizon crosses the lunch break (11:30-12:30),
// the close (15:30), or a data gap - is not labeled (FR-CAL-4), so a
// shortened horizon is never recorded as the full 5/10/15-minute outcome.
const horizonBarTolerance = 2 * time.Minute

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
// internal/service/scheduler.Handler, so it can be registered directly:
// scheduler.RegisterHandler(jobqueue.JobQueueOutcomeLabeling,
// labeler.HandleJob).
//
// The label is measured from an executable entry reference price on the
// first bar at or after the Jev response (the decision's created_at, within
// entryWaitTolerance): the ask for LONG, the bid for SHORT. From that bar
// the path runs for the job's horizon_minutes, which is the Settings timeout
// the scheduler enqueued the job with. A take-profit is touched when the
// gross return less the assumed cost reaches the TP width, a stop-loss when
// it falls to the SL width; whichever is touched first decides the label,
// and reaching neither is a timeout graded by its net return (a flat result
// is no longer "wrong"). A direction-NONE decision has no side to price, so
// it records the raw price path with no barrier fields.
//
// If the bars do not (yet) cover the path - no entry bar, or the last bar is
// more than horizonBarTolerance before the timeout - HandleJob never records
// a shortened window and never fails the job (issue #710). Within
// horizonDataGrace of the latest the data could land it persists nothing and
// returns jobqueue.Defer: the job goes back to pending and is retried after
// horizonRetryInterval; the pair also stays in PendingLabels. Past the grace
// period the gap is permanent (lunch break, close, outage): HandleJob
// records the pair as unlabelable (CalibrationRepository.MarkUnlabelable;
// still no calibration_outcomes row) and returns jobqueue.Skip, so the job
// ends succeeded with a "skipped:" note and PendingLabels stops returning
// the pair (issue #481).
func (l *Labeler) HandleJob(ctx context.Context, job jobqueue.Job) error {
	var payload calrepo.OutcomeLabelJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("calibration: decode outcome-labeling job payload: %w", err)
	}

	decision, err := l.decisions.Get(ctx, payload.JevDecisionID)
	if err != nil {
		return fmt.Errorf("calibration: load jev decision %d: %w", payload.JevDecisionID, err)
	}
	barriers, err := l.loadBarriers(ctx)
	if err != nil {
		return fmt.Errorf("calibration: load triple-barrier settings: %w", err)
	}

	direction := domain.JevDirectionNone
	if decision.Direction != nil {
		direction = *decision.Direction
	}
	responseAt := responseTime(decision)
	timeout := time.Duration(payload.HorizonMinutes) * time.Minute
	// +1s margin: SnapshotRepository.ListByInstrumentRange's `to` bound is
	// exclusive, so this still includes a bar landing exactly on the latest
	// timeout boundary.
	window, err := l.snapshots.ListByInstrumentRange(ctx, decision.InstrumentID, responseAt, responseAt.Add(entryWaitTolerance+timeout+time.Second))
	if err != nil {
		return fmt.Errorf("calibration: load market snapshots for decision %d: %w", payload.JevDecisionID, err)
	}

	entryIdx, entryRef := findEntry(window, direction, responseAt)
	if entryIdx < 0 {
		return l.shortWindow(ctx, decision, payload.HorizonMinutes, responseAt,
			"no bar with an executable entry quote within "+entryWaitTolerance.String()+" of the Jev response")
	}
	deadline := window[entryIdx].Timestamp.Add(timeout)
	path := window[entryIdx+1:]
	for len(path) > 0 && path[len(path)-1].Timestamp.After(deadline) {
		path = path[:len(path)-1]
	}
	if len(path) == 0 || path[len(path)-1].Timestamp.Before(deadline.Add(-horizonBarTolerance)) {
		reason := "no bars after the entry bar"
		if len(path) > 0 {
			reason = fmt.Sprintf("market data ends at %s, short of the timeout (lunch break, close, or data gap)", path[len(path)-1].Timestamp.Format(time.RFC3339))
		}
		return l.shortWindow(ctx, decision, payload.HorizonMinutes, responseAt, reason)
	}

	outcome := labelPath(path, entryRef, direction, barriers)
	outcome.JevDecisionID = decision.ID
	outcome.HorizonMinutes = payload.HorizonMinutes
	if _, err := l.outcomes.Insert(ctx, outcome); err != nil {
		return fmt.Errorf("calibration: persist outcome for decision %d (horizon %dm): %w", payload.JevDecisionID, payload.HorizonMinutes, err)
	}
	return nil
}

// responseTime is when Jev's response was received: the decision row is
// written right after the call returns, so its created_at, never earlier
// than the decision's bar timestamp.
func responseTime(d domain.JevDecision) time.Time {
	if d.CreatedAt.After(d.Timestamp) {
		return d.CreatedAt
	}
	return d.Timestamp
}

// findEntry returns the index of the first bar within entryWaitTolerance of
// responseAt that carries the executable entry quote, and that quote: the
// ask for LONG, the bid for SHORT, the bar's last price for a NONE decision
// (no side to price). It returns -1 when there is none.
func findEntry(window []domain.Snapshot, direction string, responseAt time.Time) (int, float64) {
	for i, bar := range window {
		if bar.Timestamp.Sub(responseAt) > entryWaitTolerance {
			break
		}
		quote := bar.Price
		switch direction {
		case domain.JevDirectionLong:
			quote = ptrOrZero(bar.Ask)
		case domain.JevDirectionShort:
			quote = ptrOrZero(bar.Bid)
		}
		if quote > 0 {
			return i, quote
		}
	}
	return -1, 0
}

func ptrOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
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

// shortWindow handles a window that does not reach the horizon, without
// ever failing the job (issue #710): while the data may still land (within
// horizonDataGrace of the latest the data could land: the Jev response +
// entryWaitTolerance + horizon) it returns
// jobqueue.Defer, so the scheduler puts the job back to pending (retried at
// the next scan cadence); once the grace has passed the gap is permanent
// and it records the skip marker and returns jobqueue.Skip, which ends the
// job as succeeded with a "skipped:" note. Either way no calibration_outcomes
// row is written, so a shortened horizon is never recorded (FR-CAL-4).
func (l *Labeler) shortWindow(ctx context.Context, decision domain.JevDecision, horizonMinutes int, responseAt time.Time, reason string) error {
	horizon := time.Duration(horizonMinutes) * time.Minute
	now := l.now()
	if now.Before(responseAt.Add(entryWaitTolerance + horizon + horizonDataGrace)) {
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

// percentReturn is the % price return from entry to price (1.0 == +1%,
// the same percentage-return convention internal/service/backtest.Trade
// already uses).
func percentReturn(entry, price float64) float64 {
	return (price - entry) / entry * 100
}

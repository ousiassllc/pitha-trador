package calibration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// DefaultHorizonsMinutes are the judgment horizons Outcome Labeling
// evaluates each Jev trader decision at (functional.md §4.12,
// docs/architecture/er.md §calibration_outcomes "horizon_minutes: 5/10/20
// 等").
var DefaultHorizonsMinutes = []int{5, 10, 20}

// horizonBarTolerance is how far before decision.Timestamp+horizon the
// last market_snapshots bar of a labeling window may fall (two 1-minute
// bars: one missed cycle plus timestamp jitter). A window that ends
// earlier - because the horizon crosses the lunch break (11:30-12:30),
// the close (15:30), or a data gap - is not labeled (FR-CAL-4), so a
// shortened horizon is never recorded as the full 5/10/20-minute outcome.
const horizonBarTolerance = 2 * time.Minute

// horizonDataGrace is how long after decision timestamp + horizon the
// labeling window may still be filling in (market-data cycle lag, a slow
// job queue) before a window that falls short of the horizon is treated as
// permanently unlabelable: bars that have not arrived by then (lunch break,
// close, outage) never will, so the pair is recorded as skipped instead of
// being retried (issue #481).
const horizonDataGrace = 5 * time.Minute

// Labeler turns one Jev trader decision (jev_decisions,
// decision_type=trader) into a calibration_outcomes row for one judgment
// horizon (functional.md FR-CAL-4): future_return, max_adverse_excursion,
// max_favorable_excursion, and was_direction_correct, computed from the
// market_snapshots bars between the decision and horizon_minutes later.
type Labeler struct {
	decisions *judgement.DecisionRepository
	snapshots *market.SnapshotRepository
	outcomes  *judgement.CalibrationRepository
	now       func() time.Time
}

// Option configures a Labeler.
type Option func(*Labeler)

// WithClock overrides the clock HandleJob uses to tell "data not landed
// yet" from "data will never land" (default time.Now), for tests.
func WithClock(now func() time.Time) Option {
	return func(l *Labeler) { l.now = now }
}

// NewLabeler returns a Labeler that loads decisions via decisions, market
// data via snapshots, and persists calibration_outcomes rows via outcomes.
func NewLabeler(decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository, outcomes *judgement.CalibrationRepository, opts ...Option) *Labeler {
	l := &Labeler{decisions: decisions, snapshots: snapshots, outcomes: outcomes, now: time.Now}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// HandleJob processes one outcome-labeling queue job
// (judgement.OutcomeLabelJobPayload): it loads the identified
// jev_decisions row, the market_snapshots bars from the decision's
// timestamp through horizon_minutes later, computes and persists the
// resulting calibration_outcomes row (FR-CAL-4). Its signature matches
// internal/service/scheduler.Handler, so it can be registered directly:
// scheduler.RegisterHandler(jobqueue.JobQueueOutcomeLabeling,
// labeler.HandleJob).
//
// If the market_snapshots bars do not (yet) reach the horizon - the last
// bar is more than horizonBarTolerance before decision timestamp +
// horizon_minutes, or the entry bar is missing - HandleJob never records a
// shortened horizon as the full-horizon outcome. Within horizonDataGrace of
// the horizon it returns an error and persists nothing (the data may still
// be landing), so internal/service/scheduler.EnqueueOutcomeLabeling's next
// periodic scan re-enqueues the pair - no separate retry queue is needed.
// Past the grace period the gap is permanent (lunch break, close, outage):
// HandleJob records the pair as unlabelable
// (CalibrationRepository.MarkUnlabelable; still no calibration_outcomes
// row) and succeeds, so PendingLabels stops returning it (issue #481).
func (l *Labeler) HandleJob(ctx context.Context, job jobqueue.Job) error {
	var payload judgement.OutcomeLabelJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("calibration: decode outcome-labeling job payload: %w", err)
	}

	decision, err := l.decisions.Get(ctx, payload.JevDecisionID)
	if err != nil {
		return fmt.Errorf("calibration: load jev decision %d: %w", payload.JevDecisionID, err)
	}

	horizon := time.Duration(payload.HorizonMinutes) * time.Minute
	// +1s margin: SnapshotRepository.ListByInstrumentRange's `to` bound is
	// exclusive, so this still includes the bar landing exactly on the
	// horizon boundary.
	window, err := l.snapshots.ListByInstrumentRange(ctx, decision.InstrumentID, decision.Timestamp, decision.Timestamp.Add(horizon+time.Second))
	if err != nil {
		return fmt.Errorf("calibration: load market snapshots for decision %d: %w", payload.JevDecisionID, err)
	}
	if len(window) < 2 || !window[0].Timestamp.Equal(decision.Timestamp) {
		return l.shortWindow(ctx, decision, payload.HorizonMinutes, "no entry bar or no later bars in the horizon window")
	}
	if last := window[len(window)-1].Timestamp; last.Before(decision.Timestamp.Add(horizon - horizonBarTolerance)) {
		return l.shortWindow(ctx, decision, payload.HorizonMinutes,
			fmt.Sprintf("market data ends at %s, short of the horizon (lunch break, close, or data gap)", last.Format(time.RFC3339)))
	}

	direction := domain.JevDirectionNone
	if decision.Direction != nil {
		direction = *decision.Direction
	}
	factor := 1.0
	if direction == domain.JevDirectionShort {
		factor = -1.0
	}

	entryPrice := window[0].Price
	mfe, mae := 0.0, 0.0
	for _, snap := range window[1:] {
		adjusted := percentReturn(entryPrice, snap.Price) * factor
		if adjusted > mfe {
			mfe = adjusted
		}
		if adjusted < mae {
			mae = adjusted
		}
	}
	futureReturn := percentReturn(entryPrice, window[len(window)-1].Price)

	var wasDirectionCorrect *bool
	if direction != domain.JevDirectionNone {
		correct := (direction == domain.JevDirectionLong && futureReturn > 0) ||
			(direction == domain.JevDirectionShort && futureReturn < 0)
		wasDirectionCorrect = &correct
	}

	if _, err := l.outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID:         decision.ID,
		HorizonMinutes:        payload.HorizonMinutes,
		FutureReturn:          futureReturn,
		MaxAdverseExcursion:   mae,
		MaxFavorableExcursion: mfe,
		WasDirectionCorrect:   wasDirectionCorrect,
	}); err != nil {
		return fmt.Errorf("calibration: persist outcome for decision %d (horizon %dm): %w", payload.JevDecisionID, payload.HorizonMinutes, err)
	}
	return nil
}

// shortWindow handles a window that does not reach the horizon: an error
// (retried by the next scan) while the data may still land, or - once
// horizonDataGrace has passed since decision timestamp + horizon - a
// permanent skip marker and nil. Either way no calibration_outcomes row is
// written, so a shortened horizon is never recorded (FR-CAL-4).
func (l *Labeler) shortWindow(ctx context.Context, decision domain.JevDecision, horizonMinutes int, reason string) error {
	horizon := time.Duration(horizonMinutes) * time.Minute
	if l.now().Before(decision.Timestamp.Add(horizon + horizonDataGrace)) {
		return fmt.Errorf("calibration: decision %d horizon %dm not labelable yet: %s", decision.ID, horizonMinutes, reason)
	}
	if err := l.outcomes.MarkUnlabelable(ctx, decision.ID, horizonMinutes, reason); err != nil {
		return fmt.Errorf("calibration: %w", err)
	}
	slog.Info("calibration: decision horizon permanently unlabelable, skipping (no shortened-horizon outcome recorded)",
		"jev_decision_id", decision.ID, "horizon_minutes", horizonMinutes, "reason", reason)
	return nil
}

// percentReturn is the % price return from entry to price (1.0 == +1%,
// the same percentage-return convention internal/service/backtest.Trade
// already uses).
func percentReturn(entry, price float64) float64 {
	return (price - entry) / entry * 100
}

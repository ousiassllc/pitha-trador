package calibration

import (
	"context"
	"encoding/json"
	"fmt"
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

// Labeler turns one Jev trader decision (jev_decisions,
// decision_type=trader) into a calibration_outcomes row for one judgment
// horizon (functional.md FR-CAL-4): future_return, max_adverse_excursion,
// max_favorable_excursion, and was_direction_correct, computed from the
// market_snapshots bars between the decision and horizon_minutes later.
type Labeler struct {
	decisions *judgement.DecisionRepository
	snapshots *market.SnapshotRepository
	outcomes  *judgement.CalibrationRepository
}

// NewLabeler returns a Labeler that loads decisions via decisions, market
// data via snapshots, and persists calibration_outcomes rows via outcomes.
func NewLabeler(decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository, outcomes *judgement.CalibrationRepository) *Labeler {
	return &Labeler{decisions: decisions, snapshots: snapshots, outcomes: outcomes}
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
// If fewer than horizon_minutes' worth of market_snapshots bars have been
// recorded yet for this decision, HandleJob returns an error and persists
// nothing: internal/service/scheduler.EnqueueOutcomeLabeling only
// enqueues a job once a decision's horizon has elapsed, and won't have
// recorded a calibration_outcomes row for it yet either, so its next
// periodic scan re-enqueues this same (jev_decision_id, horizon_minutes)
// pair - no separate retry queue is needed.
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
		return fmt.Errorf("calibration: insufficient market data to label decision %d at horizon %dm yet", payload.JevDecisionID, payload.HorizonMinutes)
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

// percentReturn is the % price return from entry to price (1.0 == +1%,
// the same percentage-return convention internal/service/backtest.Trade
// already uses).
func percentReturn(entry, price float64) float64 {
	return (price - entry) / entry * 100
}

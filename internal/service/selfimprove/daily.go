package selfimprove

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// DirectionCalibrationSource supplies the recent per-direction Calibration
// metrics Sol's daily analysis reads (FR-SELFIMPROVE-1).
// *internal/service/calibration.Service implements it.
type DirectionCalibrationSource interface {
	DirectionMetricsSince(ctx context.Context, since time.Time) (long, short domain.CalibrationMetrics, err error)
}

// DailyResult summarizes one RunDaily batch.
type DailyResult struct {
	// RolledBack lists applied proposals FR-SELFIMPROVE-6 reverted today.
	RolledBack []int64
	// Proposal is today's new Sol proposal, nil when Sol proposed
	// nothing.
	Proposal *domain.PolicyProposal
	// Applied reports whether Opus approved (and Governor applied)
	// Proposal.
	Applied bool
}

// RunDaily is the Continuous Loop's daily post-close batch
// (functional.md §4.14, overview.md §8): it first runs
// FR-SELFIMPROVE-6's post-apply check on every applied proposal (reverting
// degraded ones), then has Sol analyze the last shadowBacktestLookbackDays
// business days of Calibration data per direction (FR-SELFIMPROVE-1) and,
// when Sol proposes a change, runs the shadow backtest + Opus review and
// applies an approved proposal immediately (FR-SELFIMPROVE-4/5).
//
// Rollback runs first so today's analysis starts from the thresholds that
// actually remain in effect. A single proposal's rollback-check failure is
// joined into the returned error without stopping the rest of the batch.
func (g *Governor) RunDaily(ctx context.Context, calibration DirectionCalibrationSource) (DailyResult, error) {
	var result DailyResult

	applied, err := g.proposals.ListByStatus(ctx, domain.PolicyProposalStatusApplied)
	if err != nil {
		return result, fmt.Errorf("selfimprove: list applied proposals: %w", err)
	}
	var errs []error
	for _, p := range applied {
		rolledBack, err := g.TrackAndRollback(ctx, p.ID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if rolledBack {
			result.RolledBack = append(result.RolledBack, p.ID)
		}
	}

	since := businessDaysBefore(g.now(), shadowBacktestLookbackDays)
	long, short, err := calibration.DirectionMetricsSince(ctx, since)
	if err != nil {
		return result, errors.Join(append(errs, fmt.Errorf("selfimprove: load calibration metrics: %w", err))...)
	}
	proposal, ok, err := g.ProposeDaily(ctx, long, short)
	if err != nil {
		return result, errors.Join(append(errs, err)...)
	}
	if !ok {
		return result, errors.Join(errs...)
	}
	result.Proposal = &proposal

	approved, err := g.EvaluateProposal(ctx, proposal.ID)
	if err != nil {
		return result, errors.Join(append(errs, err)...)
	}
	result.Applied = approved
	return result, errors.Join(errs...)
}

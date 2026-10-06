package selfimprove

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// MinCalibrationSamples is the fewest labeled Calibration samples (LONG +
// SHORT, over the last shadowBacktestLookbackDays business days) Sol's
// daily analysis needs: with less, the metrics it would weigh are noise and
// the day is skipped (logged at info level, not a SkippedStages entry,
// since no AI API failed).
const MinCalibrationSamples = 30

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
	// RetriedApplied lists earlier proposals today's retry applied:
	// pending ones (whose Opus review failed on a previous day) that were
	// approved and applied, and approved ones (whose apply failed
	// part-way) that were finished.
	RetriedApplied []int64
	// Proposal is today's new Sol proposal, nil when Sol proposed nothing
	// (or its API was skipped). It may be status=rejected
	// (FR-SELFIMPROVE-8).
	Proposal *domain.PolicyProposal
	// Applied reports whether Opus approved (and Governor applied)
	// Proposal.
	Applied bool
	// SkippedStages lists the AI stages ("sol"/"opus") skipped today
	// because their external API failed or is not configured; they are
	// retried the next business day.
	SkippedStages []string
}

// RunDaily is the Continuous Loop's daily post-close batch
// (functional.md §4.14, overview.md §8): it first
// finishes the apply of any proposal an earlier failed apply left
// approved (FR-SELFIMPROVE-5), runs FR-SELFIMPROVE-6's post-apply check on
// every applied proposal (reverting degraded ones), retries the Opus
// review of any proposal an earlier Opus API failure left pending, then
// has Sol analyze the last
// shadowBacktestLookbackDays business days of Calibration data per
// direction (FR-SELFIMPROVE-1) and, when Sol proposes a change that passes
// FR-SELFIMPROVE-8's machine validation, runs the shadow backtest + Opus
// review and applies an approved proposal immediately
// (FR-SELFIMPROVE-4/5/9).
//
// Finishing approved applies and the rollback check run first so today's analysis starts from the thresholds that
// actually remain in effect. A Sol/Opus API failure skips only that stage
// for today: it is recorded in DailyResult.SkippedStages, notified via
// Notifier.AIStageSkipped, and is not an error. While a proposal remains
// pending (or approved but not yet applied) at the start of a run, that
// run only finishes/retries them and does not
// start a new Sol analysis, so two proposals never compete for the same
// thresholds. A single proposal's failure is joined into the
// returned error without stopping the rest of the batch.
func (g *Governor) RunDaily(ctx context.Context, calibration DirectionCalibrationSource) (DailyResult, error) {
	var result DailyResult

	// An approved proposal is one whose apply failed part-way
	// (EvaluateProposal approves, then applies): finish it first so it
	// is tracked and rolled back like any applied proposal below.
	var errs []error
	approved, err := g.proposals.ListByStatus(ctx, domain.PolicyProposalStatusApproved)
	if err != nil {
		return result, fmt.Errorf("selfimprove: list approved proposals: %w", err)
	}
	for _, p := range approved {
		if err := g.ApplyApproved(ctx, p.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		result.RetriedApplied = append(result.RetriedApplied, p.ID)
	}

	applied, err := g.proposals.ListByStatus(ctx, domain.PolicyProposalStatusApplied)
	if err != nil {
		return result, fmt.Errorf("selfimprove: list applied proposals: %w", err)
	}
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

	pending, err := g.proposals.ListByStatus(ctx, domain.PolicyProposalStatusPending)
	if err != nil {
		return result, errors.Join(append(errs, fmt.Errorf("selfimprove: list pending proposals: %w", err))...)
	}
	for _, p := range pending {
		ok, err := g.EvaluateProposal(ctx, p.ID)
		if err != nil {
			errs = g.skipOrCollect(ctx, &result, errs, err)
			continue
		}
		if ok {
			result.RetriedApplied = append(result.RetriedApplied, p.ID)
		}
	}
	if len(pending) > 0 || len(approved) > 0 {
		// Today's slot went to clearing the backlog: a fresh Sol
		// analysis waits until nothing is pending or half-applied at
		// the start of a run, so two proposals never compete for the
		// same thresholds.
		return result, errors.Join(errs...)
	}

	since := businessDaysBefore(g.now(), shadowBacktestLookbackDays)
	long, short, err := calibration.DirectionMetricsSince(ctx, since)
	if err != nil {
		return result, errors.Join(append(errs, fmt.Errorf("selfimprove: load calibration metrics: %w", err))...)
	}
	if total := long.SampleCount + short.SampleCount; total < MinCalibrationSamples {
		slog.InfoContext(ctx, "selfimprove: skipping Sol analysis: too few labeled calibration samples",
			"samples", total, "min_samples", MinCalibrationSamples, "since", since)
		return result, errors.Join(errs...)
	}
	proposal, proposed, err := g.ProposeDaily(ctx, long, short)
	if err != nil {
		return result, errors.Join(g.skipOrCollect(ctx, &result, errs, err)...)
	}
	if !proposed {
		return result, errors.Join(errs...)
	}
	result.Proposal = &proposal
	if proposal.Status != domain.PolicyProposalStatusPending {
		return result, errors.Join(errs...)
	}

	approvedNow, err := g.EvaluateProposal(ctx, proposal.ID)
	if err != nil {
		return result, errors.Join(g.skipOrCollect(ctx, &result, errs, err)...)
	}
	result.Applied = approvedNow
	return result, errors.Join(errs...)
}

// skipOrCollect handles one batch step's error: an external AI API failure
// (*aiStageError) is recorded as a skipped stage and, unless the API is
// simply not configured, notified; any other error is appended to errs.
func (g *Governor) skipOrCollect(ctx context.Context, result *DailyResult, errs []error, err error) []error {
	var stageErr *aiStageError
	if !errors.As(err, &stageErr) {
		return append(errs, err)
	}
	result.SkippedStages = append(result.SkippedStages, stageErr.stage)
	if errors.Is(err, assist.ErrNotConfigured) {
		slog.InfoContext(ctx, "selfimprove: AI stage not configured, skipped", "stage", stageErr.stage)
		return errs
	}
	slog.ErrorContext(ctx, "selfimprove: AI stage failed, skipped until next business day", "stage", stageErr.stage, "error", err)
	if notifyErr := g.notifier.AIStageSkipped(ctx, stageErr.stage, stageErr.err); notifyErr != nil {
		slog.ErrorContext(ctx, "selfimprove: notify skipped AI stage failed", "stage", stageErr.stage, "error", notifyErr)
	}
	return errs
}

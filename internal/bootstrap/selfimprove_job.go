package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// handleSelfImprove is the analytics queue Handler: the daily
// Continuous Loop batch Scheduler's post-close selfImproveCronSpec trigger
// enqueues (functional.md §4.14). It runs Governor.RunDaily over the
// recorded Calibration outcomes and logs what changed.
func (s *Services) handleSelfImprove(ctx context.Context, _ repository.Job) error {
	result, err := s.Governor.RunDaily(ctx, s.Calibration)
	var proposalID int64
	if result.Proposal != nil {
		proposalID = result.Proposal.ID
	}
	slog.InfoContext(ctx, "bootstrap: self-improvement daily batch finished",
		"rolled_back_proposal_ids", result.RolledBack, "retried_applied_proposal_ids", result.RetriedApplied,
		"proposal_id", proposalID, "applied", result.Applied, "skipped_ai_stages", result.SkippedStages)
	if err != nil {
		return fmt.Errorf("bootstrap: self-improvement daily batch: %w", err)
	}
	return nil
}

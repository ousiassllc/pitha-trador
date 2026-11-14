package judgement

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// InsertSkip records that the arm was not asked about the live decision
// (jev_trial_skips). It is idempotent per (decision, arm): a second skip of the
// same pair is ignored, the first reason stays. An unknown reason is an error.
// It never writes a result row of the trial: a skipped decision has no answer,
// status or cost.
func (r *TrialRepository) InsertSkip(ctx context.Context, s jevtrial.Skip) error {
	if !jevtrial.ValidSkipReason(s.Reason) {
		return fmt.Errorf("repository: jev trial skip of decision %d (arm %s): unknown reason %q", s.JevDecisionID, s.ArmID, s.Reason)
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if _, err := r.db.ExecContext(ctx, `
INSERT OR IGNORE INTO jev_trial_skips (jev_decision_id, arm_id, reason, created_at) VALUES (?, ?, ?, ?)`,
		s.JevDecisionID, s.ArmID, s.Reason, sqlutil.FormatTime(s.CreatedAt)); err != nil {
		return fmt.Errorf("repository: insert jev trial skip for decision %d (arm %s): %w", s.JevDecisionID, s.ArmID, err)
	}
	return nil
}

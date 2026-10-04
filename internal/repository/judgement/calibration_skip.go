package judgement

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// MarkUnlabelable records that the (jevDecisionID, horizonMinutes) pair can
// never be labeled because its market_snapshots window will not reach the
// horizon (lunch break, close, or a data gap that will never fill in;
// functional.md FR-CAL-4, issue #481). It writes a calibration_label_skips
// row - never a calibration_outcomes row, so no shortened-horizon outcome is
// recorded - and PendingLabels stops returning the pair, so the scheduler
// stops re-enqueuing it. Marking an already-marked pair is a no-op.
func (r *CalibrationRepository) MarkUnlabelable(ctx context.Context, jevDecisionID int64, horizonMinutes int, reason string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT OR IGNORE INTO calibration_label_skips (jev_decision_id, horizon_minutes, reason, created_at)
VALUES (?, ?, ?, ?)`,
		jevDecisionID, horizonMinutes, reason, sqlutil.FormatTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("repository: mark decision %d (horizon %dm) unlabelable: %w", jevDecisionID, horizonMinutes, err)
	}
	return nil
}

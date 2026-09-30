package risk

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Manual-resume baselines (FR-RISK-2/FR-RISK-7, issues #165/#172).
//
// consecutive_losses and daily_loss_limit are manual-resume-only, and the
// positions that tripped them stay in the history forever. Without a
// baseline the first Check after Resume would recount the same losses,
// re-fire the Kill Switch (force-closing everything again) and, since new
// entries stay blocked, no winning trade could ever end the streak.
//
// So when Resume resolves one of those events it records the resume time,
// and the matching limit is then measured only from positions closed after
// it:
//
//   - consecutive_losses: the loss streak (and the cooldown_after_loss gate
//     derived from the latest loss) restarts from zero at the resume.
//     The streak otherwise carries across trading days.
//
//   - daily_loss_limit: realized loss counts only positions closed after
//     the resume; the unrealized loss of positions still open always
//     counts, so a real drawdown still re-fires. The day rollover resets
//     it anyway.
//
//   - fill_discrepancy: orphan fills older than the resume are ignored;
//     a position whose entry order contradicts it is still checked (the
//     forced liquidation already closed it).
//
// The baselines are independent: resuming from a streak does not forgive
// the day's realized loss, and vice versa. Resuming from Paused or from a
// non-limit reason (e.g. manual Kill) moves neither.
const (
	settingKeyLossStreakBaselineAt = "system.loss_streak_baseline_at"
	settingKeyDailyLossBaselineAt  = "system.daily_loss_baseline_at"
	// settingKeyFillDiscrepancyBaselineAt (issue #185): orphan FILLED
	// orders (CheckPositionReconciliation's reverse direction) filled
	// before this time no longer raise fill_discrepancy, so a Resume is
	// not undone by the same orphan while it is still inside
	// orphanFillLookback.
	settingKeyFillDiscrepancyBaselineAt = "system.fill_discrepancy_baseline_at"
)

// baselineKeyForReason maps a manual-resume limit reason to the baseline
// Resume records when resolving it.
var baselineKeyForReason = map[string]string{
	domain.KillReasonConsecutiveLosses: settingKeyLossStreakBaselineAt,
	domain.KillReasonDailyLossLimit:    settingKeyDailyLossBaselineAt,
	domain.KillReasonFillDiscrepancy:   settingKeyFillDiscrepancyBaselineAt,
}

// baselineAt returns the recorded baseline for key, or the zero time when
// no Resume has set one.
func (e *Engine) baselineAt(ctx context.Context, key string) (time.Time, error) {
	t, _, err := e.getTimeSetting(ctx, key)
	return t, err
}

// recordResumeBaselines stores at as the baseline of every limit whose
// Kill Switch events are being resolved by a manual Resume.
func (e *Engine) recordResumeBaselines(ctx context.Context, events []domain.KillSwitchEvent, at time.Time) error {
	written := map[string]bool{}
	for _, ev := range events {
		key, ok := baselineKeyForReason[ev.Reason]
		if !ok || written[key] {
			continue
		}
		if err := e.setStringSetting(ctx, key, at.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		written[key] = true
	}
	return nil
}

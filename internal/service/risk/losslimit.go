package risk

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// The daily_loss_limit / consecutive_losses Kill Switches (FR-RISK-2/3)
// are evaluated in two places that share the helpers below: Check, on
// every Policy Engine candidate, and RunPeriodicChecks (issue #191), so a
// limit reached without any eligible signal still raises the Kill Switch,
// its notification and the forced liquidation. Both read the manual-resume
// baselines (baseline.go) and trigger idempotently (triggerIfNotActive).

// dailyLossBreached reports today's loss percentage and whether it has
// reached limits.MaxDailyLossPct.
func (e *Engine) dailyLossBreached(ctx context.Context) (float64, bool, error) {
	since, err := e.baselineAt(ctx, settingKeyDailyLossBaselineAt)
	if err != nil {
		return 0, false, fmt.Errorf("risk: read daily loss baseline: %w", err)
	}
	pct, err := e.portfolio.DailyLossPct(ctx, since)
	if err != nil {
		return 0, false, fmt.Errorf("risk: read daily loss pct: %w", err)
	}
	return pct, pct >= e.limits.MaxDailyLossPct, nil
}

func (e *Engine) triggerDailyLoss(ctx context.Context, pct float64) error {
	_, err := e.triggerIfNotActive(ctx, domain.KillReasonDailyLossLimit, map[string]any{
		"daily_loss_pct": pct, "max_daily_loss_pct": e.limits.MaxDailyLossPct,
	})
	return err
}

// CheckDailyLossLimit raises a daily_loss_limit Kill Switch (FR-RISK-2,
// force-closing every position, FR-RISK-3) once today's realized +
// unrealized loss reaches max_daily_loss_pct. RunPeriodicChecks calls it.
func (e *Engine) CheckDailyLossLimit(ctx context.Context) error {
	pct, breached, err := e.dailyLossBreached(ctx)
	if err != nil || !breached {
		return err
	}
	return e.triggerDailyLoss(ctx, pct)
}

// consecutiveLossesBreached reports the current loss streak since (the
// manual-resume baseline) and whether it has reached
// limits.MaxConsecutiveLosses.
func (e *Engine) consecutiveLossesBreached(ctx context.Context, since time.Time) (int, bool, error) {
	losses, err := e.portfolio.ConsecutiveLosses(ctx, since)
	if err != nil {
		return 0, false, fmt.Errorf("risk: read consecutive losses: %w", err)
	}
	return losses, losses >= e.limits.MaxConsecutiveLosses, nil
}

func (e *Engine) triggerConsecutiveLosses(ctx context.Context, losses int) error {
	_, err := e.triggerIfNotActive(ctx, domain.KillReasonConsecutiveLosses, map[string]any{
		"consecutive_losses": losses, "max_consecutive_losses": e.limits.MaxConsecutiveLosses,
	})
	return err
}

// CheckConsecutiveLosses raises a consecutive_losses Kill Switch
// (FR-RISK-2, force-closing every position, FR-RISK-3) once
// max_consecutive_losses losing trades have closed in a row since the last
// manual Resume. RunPeriodicChecks calls it.
func (e *Engine) CheckConsecutiveLosses(ctx context.Context) error {
	since, err := e.baselineAt(ctx, settingKeyLossStreakBaselineAt)
	if err != nil {
		return fmt.Errorf("risk: read loss streak baseline: %w", err)
	}
	losses, breached, err := e.consecutiveLossesBreached(ctx, since)
	if err != nil || !breached {
		return err
	}
	return e.triggerConsecutiveLosses(ctx, losses)
}

// RetryForceClose re-runs the forced liquidation (issue #186) while a
// force-close Kill Switch (forceCloseReasons) is unresolved and positions
// are still open: TriggerKillSwitch attempts CloseAll only once, and
// triggerIfNotActive never repeats it for an already-active reason, so a
// transient CloseAll failure would otherwise leave positions open while
// Killed. RunPeriodicChecks calls it every minute, which is the retry
// cadence; CloseAll is idempotent for already-closed positions.
func (e *Engine) RetryForceClose(ctx context.Context) error {
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	reason := ""
	for _, ev := range events {
		if forceCloseReasons[ev.Reason] {
			reason = ev.Reason
			break
		}
	}
	if reason == "" {
		return nil
	}
	open, err := e.portfolio.OpenPositionCount(ctx)
	if err != nil {
		return fmt.Errorf("risk: read open position count for force-close retry: %w", err)
	}
	if open == 0 {
		return nil
	}
	slog.Warn("risk: retrying forced liquidation", "reason", reason, "open_positions", open)
	if err := e.closer.CloseAll(ctx, reason); err != nil {
		return fmt.Errorf("risk: retry close positions after kill switch %q: %w", reason, err)
	}
	return nil
}

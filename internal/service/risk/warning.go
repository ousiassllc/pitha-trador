package risk

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// dailyLossWarningThresholdPct is non-functional.md §5.2's "日次損失上限
// 接近（例: 上限の80%到達）" threshold, expressed as a fraction of
// limits.MaxDailyLossPct.
const dailyLossWarningThresholdPct = 0.8

// CheckDailyLossWarning implements non-functional.md §5.2's daily-loss
// early-warning alert: once today's realized+unrealized loss reaches
// dailyLossWarningThresholdPct of limits.MaxDailyLossPct (the same
// FR-RISK-2 daily_loss_limit Kill Switch trigger's own threshold, engine.
// go's Check), it notifies exactly once per UTC calendar day - the
// dedup key is "already warned today", not an unresolved
// kill_switch_events row (unlike triggerIfNotActive's per-reason latch),
// since this is a warning rather than a Kill Switch trigger and today's
// loss itself resets at midnight.
//
// A later sub-scope's scheduler wiring calls this periodically (same
// deferred-wiring precedent as AutoResume/CheckHeartbeatTimeout,
// autoresume.go); nothing in this build registers that periodic trigger
// yet.
func (e *Engine) CheckDailyLossWarning(ctx context.Context) error {
	pct, err := e.portfolio.DailyLossPct(ctx)
	if err != nil {
		return fmt.Errorf("risk: read daily loss pct: %w", err)
	}
	if pct < dailyLossWarningThresholdPct*e.limits.MaxDailyLossPct {
		return nil
	}

	now := e.now()
	lastWarned, ok, err := e.getTimeSetting(ctx, settingKeyDailyLossWarningNotifiedAt)
	if err != nil {
		return err
	}
	if ok && sameUTCDate(lastWarned, now) {
		return nil
	}

	// Same best-effort rationale as TriggerKillSwitch's own Notifier call
	// (state.go): a Slack outage must not block recording that the
	// operator was warned today.
	if err := e.notifier.DailyLossWarning(ctx, pct, e.limits.MaxDailyLossPct); err != nil {
		slog.Error("risk: daily loss warning notification failed", "error", err)
	}
	return e.setStringSetting(ctx, settingKeyDailyLossWarningNotifiedAt, now.UTC().Format(time.RFC3339))
}

func sameUTCDate(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

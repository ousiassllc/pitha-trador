package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// AutoResume resolves every unresolved kill_switch_events row whose
// reason is FR-RISK-7 auto-resumable (market_data_down, jev_api_down,
// operator_heartbeat_timeout) and whose recovery condition currently
// holds, returning how many it resolved. A later sub-scope's scheduler
// wiring calls this periodically (docs/architecture/overview.md §10.3's
// "発動条件の解消を定期監視"); nothing in this build registers that
// periodic trigger yet (mirrors internal/service/scheduler.Scheduler.
// Start's own deferred-wiring precedent for cycles with no producer yet).
func (e *Engine) AutoResume(ctx context.Context) (int, error) {
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return 0, fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	resolvedAt := e.now()
	resolved := 0
	for _, ev := range events {
		if !autoResumableReasons[ev.Reason] {
			continue
		}
		recovered, err := e.recovered(ctx, ev.Reason)
		if err != nil {
			return resolved, err
		}
		if !recovered {
			continue
		}
		if err := e.killSwitch.Resolve(ctx, ev.ID, resolvedAt, domain.ResolvedByAuto); err != nil {
			return resolved, fmt.Errorf("risk: auto-resolve kill switch event %d (%s): %w", ev.ID, ev.Reason, err)
		}
		resolved++
	}
	return resolved, nil
}

func (e *Engine) recovered(ctx context.Context, reason string) (bool, error) {
	switch reason {
	case domain.KillReasonMarketDataDown:
		return e.marketDataHealth.Healthy(ctx)
	case domain.KillReasonJevAPIDown:
		return e.jevAPIHealth.Healthy(ctx)
	case domain.KillReasonOperatorHeartbeatTimeout:
		return e.heartbeatFresh(ctx)
	default:
		return false, nil
	}
}

// RecordHeartbeat implements FR-RISK-6's "認証済みUIリクエストのたびに
// last_ui_heartbeat_at を更新する" write side; internal/web/middleware's
// Heartbeat middleware calls it once per authenticated request.
func (e *Engine) RecordHeartbeat(ctx context.Context, at time.Time) error {
	return e.setStringSetting(ctx, SettingKeyLastUIHeartbeatAt, at.UTC().Format(time.RFC3339))
}

// CheckHeartbeatTimeout implements FR-RISK-6's read side (Live-only: a
// zero HeartbeatTimeoutMinutes, Paper's config/risk.yaml default, disables
// it entirely): if the operator heartbeat has gone silent for longer than
// HeartbeatTimeoutMinutes, it raises an operator_heartbeat_timeout Kill
// Switch (idempotently - see triggerIfNotActive). A later sub-scope's
// scheduler wiring calls this periodically during立会時間
// (docs/architecture/overview.md §10.4), same deferred-wiring precedent
// as AutoResume above.
func (e *Engine) CheckHeartbeatTimeout(ctx context.Context) error {
	if e.limits.HeartbeatTimeoutMinutes <= 0 {
		return nil
	}
	fresh, err := e.heartbeatFresh(ctx)
	if err != nil {
		return err
	}
	if fresh {
		return nil
	}
	_, err = e.triggerIfNotActive(ctx, domain.KillReasonOperatorHeartbeatTimeout, map[string]any{
		"heartbeat_timeout_minutes": e.limits.HeartbeatTimeoutMinutes,
	})
	return err
}

func (e *Engine) heartbeatFresh(ctx context.Context) (bool, error) {
	last, ok, err := e.getTimeSetting(ctx, SettingKeyLastUIHeartbeatAt)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	timeout := time.Duration(e.limits.HeartbeatTimeoutMinutes) * time.Minute
	return e.now().Sub(last) <= timeout, nil
}

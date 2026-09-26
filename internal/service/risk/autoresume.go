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

// CheckMarketDataHealth implements FR-RISK-2's 市場データ停止 detection:
// if cfg.MarketDataHealth reports unhealthy, it raises a
// market_data_down Kill Switch (idempotently - see triggerIfNotActive).
// This is the trigger-side counterpart to recovered's own use of the
// same HealthChecker for AutoResume's resolution check above - a single
// health signal drives both halves of FR-RISK-7's market_data_down
// auto-resume cycle. A later sub-scope's internal/service/marketdata
// health signal and scheduler wiring calls this periodically
// (docs/architecture/overview.md §10.3's "日次損失上限/連敗上限/異常検
// 知を検出"); AlwaysHealthy keeps it inert until that signal exists,
// same deferred-wiring precedent as CheckHeartbeatTimeout below.
func (e *Engine) CheckMarketDataHealth(ctx context.Context) error {
	return e.checkHealthTrigger(ctx, domain.KillReasonMarketDataDown, e.marketDataHealth)
}

// CheckJevAPIHealth implements FR-RISK-2's Jev API連続失敗 detection: if
// cfg.JevAPIHealth reports unhealthy, it raises a jev_api_down Kill
// Switch (idempotently). Same trigger/resolve pairing and deferred-wiring
// precedent as CheckMarketDataHealth above (a later
// internal/service/jev sub-scope wires a real HealthChecker in).
func (e *Engine) CheckJevAPIHealth(ctx context.Context) error {
	return e.checkHealthTrigger(ctx, domain.KillReasonJevAPIDown, e.jevAPIHealth)
}

// checkHealthTrigger is CheckMarketDataHealth/CheckJevAPIHealth's shared
// body: reason fires (idempotently) exactly when checker reports
// unhealthy.
func (e *Engine) checkHealthTrigger(ctx context.Context, reason string, checker HealthChecker) error {
	healthy, err := checker.Healthy(ctx)
	if err != nil {
		return fmt.Errorf("risk: check %s health: %w", reason, err)
	}
	if healthy {
		return nil
	}
	_, err = e.triggerIfNotActive(ctx, reason, nil)
	return err
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
// Switch (idempotently - see triggerIfNotActive).
// internal/service/scheduler.Scheduler.CheckOperatorHeartbeat
// (WithHeartbeatChecker) is the periodic caller's entry point (*Engine
// implements scheduler.HeartbeatChecker directly); actually registering a
// periodic trigger during 立会時間 in the running app
// (docs/architecture/overview.md §10.4) is still a later composition-root
// step, same deferred-wiring precedent as AutoResume above (nothing in
// this build's cmd/ composes any of Scheduler's periodic methods yet).
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

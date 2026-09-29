package risk

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// AutoResume resolves every unresolved kill_switch_events row whose
// reason is FR-RISK-7 auto-resumable (market_data_down, jev_api_down,
// operator_heartbeat_timeout) and whose recovery condition currently
// holds, returning how many it resolved.
// internal/service/scheduler.Scheduler.AutoResumeKillSwitches
// (WithAutoResumer) calls this every minute (docs/architecture/
// overview.md §10.3's "発動条件の解消を定期監視").
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
		recovered, err := e.recovered(ctx, ev)
		if err != nil {
			return resolved, err
		}
		if !recovered {
			continue
		}
		if err := e.killSwitch.Resolve(ctx, ev.ID, resolvedAt, domain.ResolvedByAuto); err != nil {
			return resolved, fmt.Errorf("risk: auto-resolve kill switch event %d (%s): %w", ev.ID, ev.Reason, err)
		}
		// Same best-effort rationale as TriggerKillSwitch's own Notifier
		// call (state.go): a Slack/Wails outage must not block resolving
		// the Kill Switch itself.
		ev.ResolvedAt = &resolvedAt
		resolvedBy := domain.ResolvedByAuto
		ev.ResolvedBy = &resolvedBy
		if err := e.notifier.KillSwitchAutoResumed(ctx, ev); err != nil {
			slog.Error("risk: kill switch auto-resume notification failed", "reason", ev.Reason, "error", err)
		}
		resolved++
	}
	return resolved, nil
}

// recovered reports whether ev's trigger condition has cleared.
func (e *Engine) recovered(ctx context.Context, ev domain.KillSwitchEvent) (bool, error) {
	switch ev.Reason {
	case domain.KillReasonMarketDataDown:
		return e.marketDataHealth.Healthy(ctx)
	case domain.KillReasonJevAPIDown:
		return e.jevAPIHealth.Healthy(ctx)
	case domain.KillReasonOperatorHeartbeatTimeout:
		return e.heartbeatRecovered(ctx, ev.TriggeredAt)
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
// auto-resume cycle. internal/service/scheduler.Scheduler.CheckRisk
// calls it every minute via RunPeriodicChecks (docs/architecture/
// overview.md §10.3's "日次損失上限/連敗上限/異常検知を検出").
func (e *Engine) CheckMarketDataHealth(ctx context.Context) error {
	return e.checkHealthTrigger(ctx, domain.KillReasonMarketDataDown, e.marketDataHealth)
}

// CheckJevAPIHealth implements FR-RISK-2's Jev API連続失敗 detection: if
// cfg.JevAPIHealth reports unhealthy, it raises a jev_api_down Kill
// Switch (idempotently). Same trigger/resolve pairing and periodic
// caller as CheckMarketDataHealth above.
func (e *Engine) CheckJevAPIHealth(ctx context.Context) error {
	return e.checkHealthTrigger(ctx, domain.KillReasonJevAPIDown, e.jevAPIHealth)
}

// checkHealthTrigger is CheckMarketDataHealth/CheckJevAPIHealth's shared
// body: reason fires (idempotently) exactly when checker reports
// unhealthy. Outside a trading session it does nothing: no board is
// fetched and no Jev call is made off-hours (non-functional.md §3), so the
// health signal would say nothing about a real outage.
func (e *Engine) checkHealthTrigger(ctx context.Context, reason string, checker HealthChecker) error {
	if !e.inSession(e.now()) {
		return nil
	}
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
// implements scheduler.HeartbeatChecker directly), run every minute
// (docs/architecture/overview.md §10.4).
func (e *Engine) CheckHeartbeatTimeout(ctx context.Context) error {
	if e.limits.HeartbeatTimeoutMinutes <= 0 || !e.inSession(e.now()) {
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
	return e.now().Sub(e.sessionHeartbeat(last, e.now())) <= timeout, nil
}

// heartbeatRecovered is FR-RISK-7's "ハートビート再検知" for an
// operator_heartbeat_timeout event: the operator has touched the UI again,
// i.e. a real heartbeat was recorded after the event fired and it is still
// within HeartbeatTimeoutMinutes. It must not use heartbeatFresh: that
// clamps a stale heartbeat up to the session open, which before the open
// makes the elapsed time negative and would release the Kill Switch every
// morning with no operator present (issue #167). The clamp is right for
// deciding to fire (silence during the session only), not for releasing.
func (e *Engine) heartbeatRecovered(ctx context.Context, triggeredAt time.Time) (bool, error) {
	last, ok, err := e.getTimeSetting(ctx, SettingKeyLastUIHeartbeatAt)
	if err != nil {
		return false, err
	}
	if !ok || !last.After(triggeredAt) {
		return false, nil
	}
	timeout := time.Duration(e.limits.HeartbeatTimeoutMinutes) * time.Minute
	return e.now().Sub(last) <= timeout, nil
}

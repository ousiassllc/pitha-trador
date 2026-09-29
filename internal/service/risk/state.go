package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// State reports the current overall system state (FR-RISK-4) and, when
// Killed, every currently-unresolved kill_switch_events row driving it.
// Killed takes priority over Paused: any unresolved Kill Switch (whether
// auto-detected, FR-RISK-2, or the manual POST /system/kill below) always
// blocks new entries regardless of the separate, unrelated manual-pause
// flag.
func (e *Engine) State(ctx context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	killed, err := e.getBoolSetting(ctx, settingKeySystemKilled)
	if err != nil {
		return "", nil, err
	}
	if len(events) > 0 || killed {
		return domain.SystemStateKilled, events, nil
	}

	paused, err := e.getBoolSetting(ctx, settingKeySystemPaused)
	if err != nil {
		return "", nil, err
	}
	if paused {
		return domain.SystemStatePaused, nil, nil
	}
	return domain.SystemStateRunning, nil, nil
}

// Pause implements POST /system/pause (docs/api/endpoints.md §4): a
// manual, Kill-Switch-independent new-entry stop the operator can lift
// with Resume at any time.
func (e *Engine) Pause(ctx context.Context) error {
	if err := e.setBoolSetting(ctx, settingKeySystemPaused, true); err != nil {
		return err
	}
	slog.Info("risk: audit: manual pause")
	return nil
}

// Kill implements POST /system/kill (docs/api/endpoints.md §4, UC-11): an
// immediate, manually-triggered Kill Switch. It is raised like any other
// trigger (TriggerKillSwitch with domain.KillReasonOperatorManual), so it
// is written to the kill_switch_events audit log (FR-RISK-5), notified
// (non-functional.md §5.2) and force-closes every open position
// (FR-RISK-3, UC-11 "強制決済"); it is manual-resume-only.
//
// The system.killed flag is set first: the block on new entries must hold
// even if the audit write or the force-close fails (Kill then returns
// that error, but the system stays Killed). It is idempotent per reason
// (triggerIfNotActive), so a repeated Kill while operator_manual is still
// unresolved adds no second row and no second flatten.
func (e *Engine) Kill(ctx context.Context) error {
	if err := e.setBoolSetting(ctx, settingKeySystemKilled, true); err != nil {
		return err
	}
	_, err := e.triggerIfNotActive(ctx, domain.KillReasonOperatorManual, map[string]any{"source": "POST /api/v1/system/kill"})
	return err
}

// Resume implements POST /system/resume (docs/api/endpoints.md §4): the
// operator's manual override back to Running from either Paused or
// Killed, regardless of which kill_switch_events reasons (if any) are
// active or whether FR-RISK-7 classifies them auto- or manual-resume-only
// - a human explicitly resuming always wins (FR-RISK-4: "人手の追加認証は
// 要求しない", i.e. no extra confirmation gate beyond this call itself).
// Resolving a consecutive_losses or daily_loss_limit event also records a
// baseline (baseline.go), so those limits count only from this moment on
// and the Kill Switch does not re-fire on the very history that caused it.
func (e *Engine) Resume(ctx context.Context) error {
	if err := e.setBoolSetting(ctx, settingKeySystemPaused, false); err != nil {
		return err
	}
	if err := e.setBoolSetting(ctx, settingKeySystemKilled, false); err != nil {
		return err
	}
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	resolvedAt := e.now()
	// The baselines are written before the events are resolved: a crash in
	// between then leaves the Kill Switch active (safe, Resume can be
	// retried) rather than resolved without a baseline (which would re-fire
	// at once, baseline.go).
	if err := e.recordResumeBaselines(ctx, events, resolvedAt); err != nil {
		return err
	}
	reasons := make([]string, 0, len(events))
	for _, ev := range events {
		if err := e.killSwitch.Resolve(ctx, ev.ID, resolvedAt, domain.ResolvedByManual); err != nil {
			return fmt.Errorf("risk: manually resolve kill switch event %d (%s): %w", ev.ID, ev.Reason, err)
		}
		reasons = append(reasons, ev.Reason)
	}
	slog.Info("risk: audit: manual resume", "resolved_reasons", reasons)
	return nil
}

// TriggerKillSwitch records reason (one of the ten domain.KillReason*
// values) as a new kill_switch_events row (FR-RISK-2, FR-RISK-5), then -
// when reason is in the forceCloseReasons set (architecture/overview.md
// §10.3) - closes every open position via Closer (FR-RISK-3). Any caller
// may invoke it directly: an automatic detector (once one of those
// sub-scopes wires in), a handler acting on an operator's explicit
// request, or Engine's own AutoResume/CheckHeartbeatTimeout below (the
// "Risk Engine内部トリガー" of the three FR-RISK-4 routes).
func (e *Engine) TriggerKillSwitch(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, error) {
	ev, err := e.recordKillSwitch(ctx, reason, detail)
	if err != nil {
		return domain.KillSwitchEvent{}, err
	}
	return ev, e.enforceKillSwitch(ctx, ev)
}

// recordKillSwitch inserts the kill_switch_events row (FR-RISK-5) without
// notifying or closing anything.
func (e *Engine) recordKillSwitch(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, error) {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("risk: encode kill switch detail for %q: %w", reason, err)
	}
	ev, err := e.killSwitch.Insert(ctx, domain.KillSwitchEvent{
		TriggeredAt: e.now(),
		Reason:      reason,
		DetailJSON:  string(detailJSON),
	})
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("risk: record kill switch event %q: %w", reason, err)
	}
	return ev, nil
}

// enforceKillSwitch notifies about ev and, for forceCloseReasons, closes
// every open position (FR-RISK-3).
func (e *Engine) enforceKillSwitch(ctx context.Context, ev domain.KillSwitchEvent) error {
	reason := ev.Reason
	// A Slack/Wails outage must never block Kill Switch enforcement
	// itself (non-functional.md §5.2 is best-effort alerting on top of
	// the enforcement path, not a precondition for it), so a Notifier
	// failure here is logged, not returned.
	if err := e.notifier.KillSwitchTriggered(ctx, ev, autoResumableReasons[reason]); err != nil {
		slog.Error("risk: kill switch notification failed", "reason", reason, "error", err)
	}
	if forceCloseReasons[reason] {
		if err := e.closer.CloseAll(ctx, reason); err != nil {
			return fmt.Errorf("risk: close positions after kill switch %q: %w", reason, err)
		}
	}
	return nil
}

// triggerIfNotActive is TriggerKillSwitch, made idempotent per reason: it
// does nothing (and returns the existing row) when an unresolved
// kill_switch_events row for reason already exists, so a limit that stays
// breached across many consecutive Check calls raises exactly one event
// rather than one per call.
//
// The "unresolved row for reason?" check and the insert run under
// triggerMu, so concurrent triggers of the same reason (Policy Engine's
// Check, the cron RunPeriodicChecks, POST /system/kill) record one row and
// notify / force-close once. Notification and CloseAll run after the lock
// is released: the row is already visible to later callers, and a slow
// CloseAll must not block unrelated triggers.
func (e *Engine) triggerIfNotActive(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, error) {
	ev, existing, err := e.recordIfNotActive(ctx, reason, detail)
	if err != nil || existing {
		return ev, err
	}
	return ev, e.enforceKillSwitch(ctx, ev)
}

func (e *Engine) recordIfNotActive(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, bool, error) {
	e.triggerMu.Lock()
	defer e.triggerMu.Unlock()
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return domain.KillSwitchEvent{}, false, fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	for _, ev := range events {
		if ev.Reason == reason {
			return ev, true, nil
		}
	}
	ev, err := e.recordKillSwitch(ctx, reason, detail)
	return ev, false, err
}

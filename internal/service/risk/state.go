package risk

import (
	"context"
	"encoding/json"
	"fmt"

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
	return e.setBoolSetting(ctx, settingKeySystemPaused, true)
}

// Kill implements POST /system/kill (docs/api/endpoints.md §4): an
// immediate, manually-triggered Kill Switch. Unlike the FR-RISK-2
// automatic triggers below, this is not logged to kill_switch_events -
// none of its nine reason values fit an operator's own, unspecified
// reason for pulling the switch (see domain.KillSwitchEvent's doc
// comment) - but it blocks new entries exactly like an automatic one
// until Resume.
func (e *Engine) Kill(ctx context.Context) error {
	return e.setBoolSetting(ctx, settingKeySystemKilled, true)
}

// Resume implements POST /system/resume (docs/api/endpoints.md §4): the
// operator's manual override back to Running from either Paused or
// Killed, regardless of which kill_switch_events reasons (if any) are
// active or whether FR-RISK-7 classifies them auto- or manual-resume-only
// - a human explicitly resuming always wins (FR-RISK-4: "人手の追加認証は
// 要求しない", i.e. no extra confirmation gate beyond this call itself).
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
	for _, ev := range events {
		if err := e.killSwitch.Resolve(ctx, ev.ID, resolvedAt, domain.ResolvedByManual); err != nil {
			return fmt.Errorf("risk: manually resolve kill switch event %d (%s): %w", ev.ID, ev.Reason, err)
		}
	}
	return nil
}

// TriggerKillSwitch records reason (one of the nine domain.KillReason*
// values) as a new kill_switch_events row (FR-RISK-2, FR-RISK-5), then -
// when reason is in the forceCloseReasons set (architecture/overview.md
// §10.3) - closes every open position via Closer (FR-RISK-3). Any caller
// may invoke it directly: an automatic detector (once one of those
// sub-scopes wires in), a handler acting on an operator's explicit
// request, or Engine's own AutoResume/CheckHeartbeatTimeout below (the
// "Risk Engine内部トリガー" of the three FR-RISK-4 routes).
func (e *Engine) TriggerKillSwitch(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, error) {
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
	if forceCloseReasons[reason] {
		if err := e.closer.CloseAll(ctx, reason); err != nil {
			return ev, fmt.Errorf("risk: close positions after kill switch %q: %w", reason, err)
		}
	}
	return ev, nil
}

// triggerIfNotActive is TriggerKillSwitch, made idempotent per reason: it
// does nothing (and returns the existing row) when an unresolved
// kill_switch_events row for reason already exists, so a limit that stays
// breached across many consecutive Check calls raises exactly one event
// rather than one per call.
func (e *Engine) triggerIfNotActive(ctx context.Context, reason string, detail map[string]any) (domain.KillSwitchEvent, error) {
	events, err := e.killSwitch.ListUnresolved(ctx)
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("risk: list unresolved kill switch events: %w", err)
	}
	for _, ev := range events {
		if ev.Reason == reason {
			return ev, nil
		}
	}
	return e.TriggerKillSwitch(ctx, reason, detail)
}

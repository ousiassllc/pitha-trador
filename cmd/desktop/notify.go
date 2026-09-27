package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// var _ risk.Notifier = (*App)(nil) documents (and enforces at compile
// time) that App is the real internal/service/risk.Notifier
// implementation for the Wails desktop shell. main.go passes App to
// bootstrap.BuildServices, which fans it out alongside the structured-log
// and Slack channels (internal/bootstrap/risk.go).
var _ risk.Notifier = (*App)(nil)

// errWailsNotStarted is returned instead of calling the Wails runtime
// before OnStartup has provided its context: every runtime.* call with a
// nil context terminates the process via log.Fatalf.
var errWailsNotStarted = errors.New("desktop: native notification unavailable before Wails startup")

// killSwitchTriggeredEvent/killSwitchResumedEvent/dailyLossWarningEvent
// are the runtime.EventsEmit channel names the frontend (Lit
// pitha-kill-switch-panel, docs/components/overview.md) subscribes to
// for its in-window status indicator. architecture/overview.md §9/§10.3
// call this a "システムトレイアイコン変化", but Wails v2's runtime
// package (github.com/wailsapp/wails/v2/pkg/runtime) has no OS-level
// system-tray API - only SendNotification (a native OS toast) and
// EventsEmit exist. This build therefore drives the in-window status
// indicator via EventsEmit; a literal OS system-tray icon would need
// either Wails v3 or a third-party systray library, neither of which
// this sub-issue introduces.
const (
	killSwitchTriggeredEvent = "kill-switch:triggered"
	killSwitchResumedEvent   = "kill-switch:auto-resumed"
	dailyLossWarningEvent    = "risk:daily-loss-warning"
)

// killSwitchNotificationBody renders the native OS toast body
// architecture/overview.md §9/§10.3's Kill Switch notification requires:
// the reason (docs/requirements/non-functional.md §5.2 "自動再開可否・
// 発動理由を含む") in Japanese via notify.reasonLabel's own vocabulary,
// plus whether it resolves itself or needs a manual resume
// (functional.md FR-RISK-7).
func killSwitchNotificationBody(reason string, autoResumable bool) string {
	resume := "手動再開が必要です"
	if autoResumable {
		resume = "発動条件の解消後に自動再開されます"
	}
	return fmt.Sprintf("reason=%s（%s）", reason, resume)
}

// KillSwitchTriggered implements risk.Notifier for the Wails desktop
// shell: it fires a native OS toast notification (runtime.SendNotification,
// Wails v2's actual native notification API) and a kill-switch:triggered
// frontend event carrying reason/autoResumable so the UI can update its
// status indicator (architecture/overview.md §9, §10.3).
func (a *App) KillSwitchTriggered(_ context.Context, ev domain.KillSwitchEvent, autoResumable bool) error {
	if a.ctx == nil {
		return errWailsNotStarted
	}
	if err := runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    "kill-switch-triggered",
		Title: "Kill Switch発動",
		Body:  killSwitchNotificationBody(ev.Reason, autoResumable),
	}); err != nil {
		return fmt.Errorf("desktop: send kill switch native notification: %w", err)
	}
	runtime.EventsEmit(a.ctx, killSwitchTriggeredEvent, map[string]any{
		"reason":        ev.Reason,
		"autoResumable": autoResumable,
		"triggeredAt":   ev.TriggeredAt,
	})
	return nil
}

// KillSwitchAutoResumed implements risk.Notifier: it fires a native OS
// toast plus a kill-switch:auto-resumed frontend event, so the UI can
// reset the status indicator KillSwitchTriggered set.
func (a *App) KillSwitchAutoResumed(_ context.Context, ev domain.KillSwitchEvent) error {
	if a.ctx == nil {
		return errWailsNotStarted
	}
	if err := runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    "kill-switch-auto-resumed",
		Title: "Kill Switch自動再開",
		Body:  fmt.Sprintf("reason=%s", ev.Reason),
	}); err != nil {
		return fmt.Errorf("desktop: send kill switch auto-resume native notification: %w", err)
	}
	runtime.EventsEmit(a.ctx, killSwitchResumedEvent, map[string]any{"reason": ev.Reason})
	return nil
}

// DailyLossWarning implements risk.Notifier's remaining method so App
// can be used as a real risk.Notifier directly. non-functional.md §5.2
// only lists this alert as Slack-bound (no native toast), so this only
// emits a frontend event for the UI to surface a warning banner.
func (a *App) DailyLossWarning(_ context.Context, currentPct, limitPct float64) error {
	if a.ctx == nil {
		return errWailsNotStarted
	}
	runtime.EventsEmit(a.ctx, dailyLossWarningEvent, map[string]any{
		"currentPct": currentPct,
		"limitPct":   limitPct,
	})
	return nil
}

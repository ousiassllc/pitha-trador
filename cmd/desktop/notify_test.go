package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestKillSwitchNotificationBody_AutoResumable(t *testing.T) {
	body := killSwitchNotificationBody(domain.KillReasonMarketDataDown, true)
	if !strings.Contains(body, domain.KillReasonMarketDataDown) {
		t.Errorf("body %q does not contain the reason", body)
	}
	if !strings.Contains(body, "自動再開") {
		t.Errorf("auto-resumable body %q should mention automatic resume", body)
	}
	if strings.Contains(body, "手動再開が必要です") {
		t.Errorf("auto-resumable body %q should not claim manual resume is required", body)
	}
}

func TestKillSwitchNotificationBody_ManualResumeOnly(t *testing.T) {
	body := killSwitchNotificationBody(domain.KillReasonUnexpectedPosition, false)
	if !strings.Contains(body, domain.KillReasonUnexpectedPosition) {
		t.Errorf("body %q does not contain the reason", body)
	}
	if !strings.Contains(body, "手動再開が必要です") {
		t.Errorf("manual-only body %q should state manual resume is required", body)
	}
}

func TestApp_NotifierBeforeStartupReturnsErrorInsteadOfExiting(t *testing.T) {
	app := NewApp()
	ctx := context.Background()
	ev := domain.KillSwitchEvent{Reason: domain.KillReasonMarketDataDown}

	if err := app.KillSwitchTriggered(ctx, ev, true); !errors.Is(err, errWailsNotStarted) {
		t.Errorf("KillSwitchTriggered before startup = %v, want errWailsNotStarted", err)
	}
	if err := app.KillSwitchAutoResumed(ctx, ev); !errors.Is(err, errWailsNotStarted) {
		t.Errorf("KillSwitchAutoResumed before startup = %v, want errWailsNotStarted", err)
	}
	if err := app.DailyLossWarning(ctx, 2.5, 3.0); !errors.Is(err, errWailsNotStarted) {
		t.Errorf("DailyLossWarning before startup = %v, want errWailsNotStarted", err)
	}
}

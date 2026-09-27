package main

import (
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

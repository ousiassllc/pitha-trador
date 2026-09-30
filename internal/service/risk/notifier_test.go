package risk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

func newEngineWithNotifier(t *testing.T, portfolio risk.PortfolioProvider, notifier risk.Notifier, now func() time.Time) *risk.Engine {
	t.Helper()
	db := newTestDB(t)
	if now == nil {
		now = time.Now
	}
	return risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
		Portfolio:  portfolio,
		Notifier:   notifier,
		Now:        now,
	})
}

func TestEngine_TriggerKillSwitch_NotifiesWithAutoResumableReason(t *testing.T) {
	notifier := &fakeNotifier{}
	e := newEngineWithNotifier(t, risk.ZeroPortfolioProvider{}, notifier, nil)
	ctx := context.Background()

	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonMarketDataDown, map[string]any{"stale_seconds": 120}); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}

	if len(notifier.triggered) != 1 {
		t.Fatalf("triggered calls = %d, want 1", len(notifier.triggered))
	}
	call := notifier.triggered[0]
	if call.event.Reason != domain.KillReasonMarketDataDown {
		t.Errorf("reason = %q, want %q", call.event.Reason, domain.KillReasonMarketDataDown)
	}
	if !call.autoResumable {
		t.Error("market_data_down should be reported as auto-resumable (FR-RISK-7)")
	}
}

func TestEngine_TriggerKillSwitch_NotifiesWithManualResumeOnlyReason(t *testing.T) {
	notifier := &fakeNotifier{}
	e := newEngineWithNotifier(t, risk.ZeroPortfolioProvider{}, notifier, nil)
	ctx := context.Background()

	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonUnexpectedPosition, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}

	if len(notifier.triggered) != 1 {
		t.Fatalf("triggered calls = %d, want 1", len(notifier.triggered))
	}
	if notifier.triggered[0].autoResumable {
		t.Error("unexpected_position should be reported as manual-resume-only (FR-RISK-7)")
	}
}

func TestEngine_TriggerKillSwitch_NotifierFailureDoesNotBlockTrigger(t *testing.T) {
	notifier := &fakeNotifier{err: errors.New("slack unreachable")}
	e := newEngineWithNotifier(t, risk.ZeroPortfolioProvider{}, notifier, nil)
	ctx := context.Background()

	ev, err := e.TriggerKillSwitch(ctx, domain.KillReasonBrokerAPIError, nil)
	if err != nil {
		t.Fatalf("TriggerKillSwitch should not fail when Notifier fails: %v", err)
	}
	if ev.Reason != domain.KillReasonBrokerAPIError {
		t.Errorf("event reason = %q, want %q", ev.Reason, domain.KillReasonBrokerAPIError)
	}
}

func TestEngine_AutoResume_NotifiesEachResolvedReason(t *testing.T) {
	notifier := &fakeNotifier{}
	e := newEngineWithNotifier(t, risk.ZeroPortfolioProvider{}, notifier, nil)
	ctx := context.Background()

	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonMarketDataDown, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}
	notifier.triggered = nil // only asserting on AutoResume's own notification below

	resolved, err := e.AutoResume(ctx)
	if err != nil {
		t.Fatalf("AutoResume: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("AutoResume resolved = %d, want 1 (AlwaysHealthy{} default reports market data healthy)", resolved)
	}
	if len(notifier.autoResumed) != 1 {
		t.Fatalf("auto-resumed calls = %d, want 1", len(notifier.autoResumed))
	}
	if notifier.autoResumed[0].Reason != domain.KillReasonMarketDataDown {
		t.Errorf("reason = %q, want %q", notifier.autoResumed[0].Reason, domain.KillReasonMarketDataDown)
	}
}

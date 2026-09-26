package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

func TestEngine_Resume_ClearsPausedKilledAndResolvesAllUnresolvedEvents(t *testing.T) {
	e, killSwitch := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	ctx := context.Background()

	if err := e.Pause(ctx); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonBrokerAPIError, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}

	state, _, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State before Resume: %v", err)
	}
	if state != domain.SystemStateKilled {
		t.Fatalf("State before Resume = %q, want %q (Killed takes priority over Paused)", state, domain.SystemStateKilled)
	}

	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	state, events, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State after Resume: %v", err)
	}
	if state != domain.SystemStateRunning || len(events) != 0 {
		t.Fatalf("State after Resume = (%q, %v), want (%q, empty)", state, events, domain.SystemStateRunning)
	}

	unresolved, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("ListUnresolved = %+v, want empty (manual-resume-only reason still resolved by Resume)", unresolved)
	}
}

func TestEngine_AutoResume_ResolvesOnlyRecoveredAutoResumableReasons(t *testing.T) {
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	e := risk.NewEngine(risk.Config{
		Limits:           testLimits(),
		KillSwitch:       killSwitch,
		Settings:         repository.NewRuntimeSettingsRepository(db),
		Portfolio:        risk.ZeroPortfolioProvider{},
		MarketDataHealth: fakeHealth{healthy: true},
		JevAPIHealth:     fakeHealth{healthy: false},
	})
	ctx := context.Background()

	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonMarketDataDown, nil); err != nil {
		t.Fatalf("TriggerKillSwitch market_data_down: %v", err)
	}
	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonJevAPIDown, nil); err != nil {
		t.Fatalf("TriggerKillSwitch jev_api_down: %v", err)
	}
	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonConsecutiveLosses, nil); err != nil {
		t.Fatalf("TriggerKillSwitch consecutive_losses: %v", err)
	}

	resolved, err := e.AutoResume(ctx)
	if err != nil {
		t.Fatalf("AutoResume: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("AutoResume resolved = %d, want 1 (only the healthy market_data_down event)", resolved)
	}

	remaining, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("ListUnresolved = %+v, want 2 remaining (jev_api_down still unhealthy, consecutive_losses is manual-only)", remaining)
	}
	for _, ev := range remaining {
		if ev.Reason == domain.KillReasonMarketDataDown {
			t.Fatalf("market_data_down should have been auto-resolved, still unresolved: %+v", ev)
		}
	}
}

func TestEngine_CheckHeartbeatTimeout_NoOpWhenLimitZero(t *testing.T) {
	e, killSwitch := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil) // HeartbeatTimeoutMinutes = 0 (Paper)
	ctx := context.Background()

	if err := e.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}

	events, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ListUnresolved = %+v, want empty (Paper mode disables FR-RISK-6)", events)
	}
}

func TestEngine_CheckHeartbeatTimeout_TriggersWhenStale_ClearsAfterRecordHeartbeat(t *testing.T) {
	limits := testLimits()
	limits.HeartbeatTimeoutMinutes = 120
	now := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	clock := now
	e, killSwitch := newEngine(t, limits, risk.ZeroPortfolioProvider{}, nil, func() time.Time { return clock })
	ctx := context.Background()

	// No heartbeat has ever been recorded: treated as stale immediately.
	if err := e.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout (no heartbeat yet): %v", err)
	}
	events, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(events) != 1 || events[0].Reason != domain.KillReasonOperatorHeartbeatTimeout {
		t.Fatalf("ListUnresolved = %+v, want exactly one operator_heartbeat_timeout event", events)
	}

	// Operator re-operates the UI: RecordHeartbeat, then AutoResume clears it.
	if err := e.RecordHeartbeat(ctx, clock); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	resolved, err := e.AutoResume(ctx)
	if err != nil {
		t.Fatalf("AutoResume: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("AutoResume resolved = %d, want 1", resolved)
	}

	state, _, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateRunning {
		t.Fatalf("State = %q, want %q", state, domain.SystemStateRunning)
	}
}

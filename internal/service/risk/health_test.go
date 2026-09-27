package risk_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// mutableHealth is a HealthChecker whose Healthy() answer can flip after
// construction, for tests that exercise a full detect (unhealthy) ->
// recover (healthy) -> AutoResume cycle in a single Engine (fakeHealth's
// value-typed, construction-time-fixed answer cannot do that).
type mutableHealth struct{ healthy bool }

func (h *mutableHealth) Healthy(context.Context) (bool, error) { return h.healthy, nil }

func TestEngine_CheckMarketDataHealth_TriggersOnceWhenUnhealthy(t *testing.T) {
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	e := risk.NewEngine(risk.Config{
		Limits:           testLimits(),
		KillSwitch:       killSwitch,
		Settings:         repository.NewRuntimeSettingsRepository(db),
		Portfolio:        risk.ZeroPortfolioProvider{},
		MarketDataHealth: fakeHealth{healthy: false},
	})
	ctx := context.Background()

	if err := e.CheckMarketDataHealth(ctx); err != nil {
		t.Fatalf("CheckMarketDataHealth: %v", err)
	}
	// A second call while still unhealthy must not raise a duplicate
	// event (triggerIfNotActive's idempotency, mirroring Check's own
	// daily_loss_limit/consecutive_losses behavior).
	if err := e.CheckMarketDataHealth(ctx); err != nil {
		t.Fatalf("CheckMarketDataHealth (2nd call): %v", err)
	}

	state, events, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateKilled {
		t.Fatalf("State = %q, want %q", state, domain.SystemStateKilled)
	}
	if len(events) != 1 || events[0].Reason != domain.KillReasonMarketDataDown {
		t.Fatalf("unresolved events = %+v, want exactly one market_data_down", events)
	}
}

func TestEngine_CheckMarketDataHealth_NoOpWhenHealthy(t *testing.T) {
	e, _ := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	ctx := context.Background()

	if err := e.CheckMarketDataHealth(ctx); err != nil {
		t.Fatalf("CheckMarketDataHealth: %v", err)
	}

	state, events, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateRunning || len(events) != 0 {
		t.Fatalf("State = (%q, %v), want (%q, empty) - newEngine's default MarketDataHealth is AlwaysHealthy", state, events, domain.SystemStateRunning)
	}
}

func TestEngine_CheckJevAPIHealth_TriggersWhenUnhealthy(t *testing.T) {
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	e := risk.NewEngine(risk.Config{
		Limits:       testLimits(),
		KillSwitch:   killSwitch,
		Settings:     repository.NewRuntimeSettingsRepository(db),
		Portfolio:    risk.ZeroPortfolioProvider{},
		JevAPIHealth: fakeHealth{healthy: false},
	})
	ctx := context.Background()

	if err := e.CheckJevAPIHealth(ctx); err != nil {
		t.Fatalf("CheckJevAPIHealth: %v", err)
	}

	state, events, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateKilled || len(events) != 1 || events[0].Reason != domain.KillReasonJevAPIDown {
		t.Fatalf("State/events = (%q, %+v), want (%q, one jev_api_down event)", state, events, domain.SystemStateKilled)
	}
}

// TestEngine_MarketDataDown_FullDetectAutoResumeCycle exercises FR-RISK-7's
// end-to-end auto-resume loop for market_data_down: CheckMarketDataHealth
// raises the Kill Switch while data is down, then - once
// MarketDataHealth reports recovered - AutoResume resolves it with
// resolved_by=auto and new entries are no longer blocked, all without any
// manual Resume call (the acceptance criterion FR-RISK-7's auto-resumable
// row of functional.md §4.7's table describes).
func TestEngine_MarketDataDown_FullDetectAutoResumeCycle(t *testing.T) {
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	health := &mutableHealth{healthy: false}
	e := risk.NewEngine(risk.Config{
		Limits:           testLimits(),
		KillSwitch:       killSwitch,
		Settings:         repository.NewRuntimeSettingsRepository(db),
		Portfolio:        risk.ZeroPortfolioProvider{},
		MarketDataHealth: health,
	})
	ctx := context.Background()

	if err := e.CheckMarketDataHealth(ctx); err != nil {
		t.Fatalf("CheckMarketDataHealth (down): %v", err)
	}
	if state, _, err := e.State(ctx); err != nil || state != domain.SystemStateKilled {
		t.Fatalf("State after down = (%q, %v), want %q", state, err, domain.SystemStateKilled)
	}
	unresolvedBefore, err := killSwitch.ListUnresolved(ctx)
	if err != nil || len(unresolvedBefore) != 1 {
		t.Fatalf("ListUnresolved before recovery = (%+v, %v), want exactly one event", unresolvedBefore, err)
	}
	eventID := unresolvedBefore[0].ID

	health.healthy = true
	resolved, err := e.AutoResume(ctx)
	if err != nil {
		t.Fatalf("AutoResume: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("AutoResume resolved = %d, want 1", resolved)
	}

	state, events, err := e.State(ctx)
	if err != nil {
		t.Fatalf("State after AutoResume: %v", err)
	}
	if state != domain.SystemStateRunning || len(events) != 0 {
		t.Fatalf("State after AutoResume = (%q, %v), want (%q, empty)", state, events, domain.SystemStateRunning)
	}

	resolvedEvent, err := killSwitch.Get(ctx, eventID)
	if err != nil {
		t.Fatalf("Get resolved event %d: %v", eventID, err)
	}
	if resolvedEvent.ResolvedAt == nil || resolvedEvent.ResolvedBy == nil || *resolvedEvent.ResolvedBy != domain.ResolvedByAuto {
		t.Fatalf("resolved event = %+v, want ResolvedAt set and ResolvedBy=%q", resolvedEvent, domain.ResolvedByAuto)
	}
}

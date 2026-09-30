package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type switchableHealth struct{ healthy bool }

func (h *switchableHealth) Healthy(context.Context) (bool, error) { return h.healthy, nil }

type recordingNotifier struct {
	risk.NoopNotifier
	triggered   []domain.KillSwitchEvent
	autoResumed []domain.KillSwitchEvent
}

func (n *recordingNotifier) KillSwitchTriggered(_ context.Context, ev domain.KillSwitchEvent, _ bool) error {
	n.triggered = append(n.triggered, ev)
	return nil
}

func (n *recordingNotifier) KillSwitchAutoResumed(_ context.Context, ev domain.KillSwitchEvent) error {
	n.autoResumed = append(n.autoResumed, ev)
	return nil
}

// TestScheduler_RiskMonitorAndAutoResume_DetectNotifyRecover drives the
// two periodic entry points (CheckRisk = the 1-minute risk-check
// trigger, AutoResumeKillSwitches = the 1-minute auto-resume trigger)
// through FR-RISK-2/FR-RISK-7's whole cycle against a real Risk Engine:
// market data stops -> market_data_down Kill Switch + notification ->
// data recovers -> the event is resolved with resolved_by=auto and the
// auto-resume notification goes out, with no manual Resume.
func TestScheduler_RiskMonitorAndAutoResume_DetectNotifyRecover(t *testing.T) {
	db := newTestDB(t)
	health := &switchableHealth{healthy: false}
	notifier := &recordingNotifier{}
	killSwitch := system.NewKillSwitchRepository(db)
	engine := risk.NewEngine(risk.Config{
		Limits:           config.RiskLimits{MaxDailyLossPct: 1.0, MaxConsecutiveLosses: 4},
		KillSwitch:       killSwitch,
		Settings:         system.NewRuntimeSettingsRepository(db),
		MarketDataHealth: health,
		Notifier:         notifier,
	})
	s := scheduler.New(jobqueue.NewJobRepository(db), market.NewInstrumentRepository(db),
		scheduler.WithRiskMonitor(engine), scheduler.WithAutoResumer(engine))
	ctx := context.Background()

	if err := s.CheckRisk(ctx); err != nil {
		t.Fatalf("CheckRisk: %v", err)
	}
	unresolved, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(unresolved) != 1 || unresolved[0].Reason != domain.KillReasonMarketDataDown {
		t.Fatalf("unresolved after CheckRisk = %+v, want one market_data_down", unresolved)
	}
	if len(notifier.triggered) != 1 {
		t.Fatalf("trigger notifications = %d, want 1", len(notifier.triggered))
	}

	// Still down: auto-resume must leave the Kill Switch in place.
	if err := s.AutoResumeKillSwitches(ctx); err != nil {
		t.Fatalf("AutoResumeKillSwitches (still down): %v", err)
	}
	if unresolved, _ = killSwitch.ListUnresolved(ctx); len(unresolved) != 1 {
		t.Fatalf("unresolved while still down = %d, want 1", len(unresolved))
	}

	health.healthy = true
	if err := s.AutoResumeKillSwitches(ctx); err != nil {
		t.Fatalf("AutoResumeKillSwitches (recovered): %v", err)
	}
	if unresolved, _ = killSwitch.ListUnresolved(ctx); len(unresolved) != 0 {
		t.Fatalf("unresolved after recovery = %+v, want none", unresolved)
	}
	events, err := killSwitch.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 || events[0].ResolvedBy == nil || *events[0].ResolvedBy != domain.ResolvedByAuto {
		t.Fatalf("events = %+v, want the one event resolved_by=auto", events)
	}
	if len(notifier.autoResumed) != 1 {
		t.Fatalf("auto-resume notifications = %d, want 1", len(notifier.autoResumed))
	}
}

type failingRiskMonitor struct{ err error }

func (f failingRiskMonitor) RunPeriodicChecks(context.Context) error { return f.err }

type failingAutoResumer struct{ err error }

func (f failingAutoResumer) AutoResume(context.Context) (int, error) { return 0, f.err }

func TestScheduler_RiskEntryPoints_NoOpWithoutDependenciesAndPropagateErrors(t *testing.T) {
	db := newTestDB(t)
	jobs, instruments := jobqueue.NewJobRepository(db), market.NewInstrumentRepository(db)
	ctx := context.Background()

	bare := scheduler.New(jobs, instruments)
	if err := bare.CheckRisk(ctx); err != nil {
		t.Fatalf("CheckRisk without monitor: %v", err)
	}
	if err := bare.AutoResumeKillSwitches(ctx); err != nil {
		t.Fatalf("AutoResumeKillSwitches without resumer: %v", err)
	}

	wantErr := errors.New("db unavailable")
	s := scheduler.New(jobs, instruments,
		scheduler.WithRiskMonitor(failingRiskMonitor{err: wantErr}),
		scheduler.WithAutoResumer(failingAutoResumer{err: wantErr}))
	if err := s.CheckRisk(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("CheckRisk error = %v, want %v", err, wantErr)
	}
	if err := s.AutoResumeKillSwitches(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("AutoResumeKillSwitches error = %v, want %v", err, wantErr)
	}
}

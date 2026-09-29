package killswitchflow_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// syncRecorder is a goroutine-safe risk.PositionCloser and risk.Notifier
// that counts Kill Switch enforcement calls.
type syncRecorder struct {
	mu        sync.Mutex
	closed    int
	triggered int
}

func (r *syncRecorder) CloseAll(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

func (r *syncRecorder) KillSwitchTriggered(context.Context, domain.KillSwitchEvent, bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggered++
	return nil
}

func (r *syncRecorder) KillSwitchAutoResumed(context.Context, domain.KillSwitchEvent) error {
	return nil
}

func (r *syncRecorder) DailyLossWarning(context.Context, float64, float64) error { return nil }

func (r *syncRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed, r.triggered = 0, 0
}

func (r *syncRecorder) counts() (triggered, closed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.triggered, r.closed
}

// dailyLossPortfolio reports a daily loss at the limit regardless of the
// resume baseline, so the daily_loss_limit Kill Switch re-fires every round.
type dailyLossPortfolio struct {
	fakePortfolio
	pct float64
}

func (p dailyLossPortfolio) DailyLossPct(context.Context, time.Time) (float64, error) {
	return p.pct, nil
}

func newRecordingEngine(t *testing.T, portfolio risk.PortfolioProvider, rec *syncRecorder) (*risk.Engine, *repository.KillSwitchRepository) {
	t.Helper()
	db := newTestDB(t)
	killSwitch := repository.NewKillSwitchRepository(db)
	e := risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: killSwitch,
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Portfolio:  portfolio,
		Closer:     rec,
		Notifier:   rec,
	})
	return e, killSwitch
}

// Issue #223: Check (Policy Engine worker), RunPeriodicChecks (cron) and
// Kill (POST /system/kill) may fire the same reason concurrently; each
// reason must still yield one event row, one notification and one CloseAll.
func TestEngine_ConcurrentTriggersOfSameReasonRecordOneEvent(t *testing.T) {
	rec := &syncRecorder{}
	e, killSwitch := newRecordingEngine(t, dailyLossPortfolio{pct: testLimits().MaxDailyLossPct}, rec)
	ctx := context.Background()

	const (
		rounds     = 30
		goroutines = 24
	)
	for round := 0; round < rounds; round++ {
		rec.reset()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				switch i % 3 {
				case 0:
					_ = e.CheckDailyLossLimit(ctx)
				case 1:
					e.Check(ctx, 1, domain.JevDirectionLong)
				default:
					_ = e.RunPeriodicChecks(ctx)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		events, err := killSwitch.ListUnresolved(ctx)
		if err != nil {
			t.Fatalf("round %d: ListUnresolved: %v", round, err)
		}
		dailyLoss := 0
		for _, ev := range events {
			if ev.Reason == domain.KillReasonDailyLossLimit {
				dailyLoss++
			}
		}
		if dailyLoss != 1 {
			t.Fatalf("round %d: unresolved %s rows = %d, want 1 (events: %+v)", round, domain.KillReasonDailyLossLimit, dailyLoss, events)
		}
		if triggered, closed := rec.counts(); triggered != len(events) || closed != len(events) {
			t.Fatalf("round %d: notifications = %d, CloseAll calls = %d, want %d each (one per event)", round, triggered, closed, len(events))
		}
		// Resolve so the next round races on a state with nothing unresolved.
		if err := e.Resume(ctx); err != nil {
			t.Fatalf("round %d: Resume: %v", round, err)
		}
	}
}

func TestEngine_ConcurrentKillRecordsOneOperatorManualEvent(t *testing.T) {
	rec := &syncRecorder{}
	e, killSwitch := newRecordingEngine(t, risk.ZeroPortfolioProvider{}, rec)
	ctx := context.Background()

	const (
		rounds     = 20
		goroutines = 16
	)
	for round := 0; round < rounds; round++ {
		rec.reset()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_ = e.Kill(ctx)
			}()
		}
		close(start)
		wg.Wait()

		events, err := killSwitch.ListUnresolved(ctx)
		if err != nil {
			t.Fatalf("round %d: ListUnresolved: %v", round, err)
		}
		if triggered, closed := rec.counts(); len(events) != 1 || triggered != 1 || closed != 1 {
			t.Fatalf("round %d: events = %d, notifications = %d, CloseAll = %d, want 1 each", round, len(events), triggered, closed)
		}
		if err := e.Resume(ctx); err != nil {
			t.Fatalf("round %d: Resume: %v", round, err)
		}
	}
}

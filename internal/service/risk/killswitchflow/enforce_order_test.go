package killswitchflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// orderRecorder logs the order of CloseAll / KillSwitchTriggered calls.
type orderRecorder struct {
	mu       sync.Mutex
	calls    []string
	closeErr error
}

func (r *orderRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *orderRecorder) CloseAll(context.Context, string) error {
	r.add("close")
	return r.closeErr
}

func (r *orderRecorder) KillSwitchTriggered(context.Context, domain.KillSwitchEvent, bool) error {
	r.add("notify")
	return nil
}

func (r *orderRecorder) KillSwitchAutoResumed(context.Context, domain.KillSwitchEvent) error {
	return nil
}

func (r *orderRecorder) DailyLossWarning(context.Context, float64, float64) error { return nil }

func newOrderEngine(t *testing.T, rec *orderRecorder) *risk.Engine {
	t.Helper()
	db := newTestDB(t)
	return risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
		Portfolio:  fakePortfolio{},
		Closer:     rec,
		Notifier:   rec,
	})
}

// #628: the (possibly 10s-per-channel slow) notification must not delay the
// force-close of open positions.
func TestEngine_TriggerKillSwitch_ClosesPositionsBeforeNotifying(t *testing.T) {
	rec := &orderRecorder{}
	e := newOrderEngine(t, rec)

	if _, err := e.TriggerKillSwitch(context.Background(), domain.KillReasonDailyLossLimit, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}
	if got := rec.calls; len(got) != 2 || got[0] != "close" || got[1] != "notify" {
		t.Fatalf("calls = %v, want [close notify]", got)
	}
}

// A failing CloseAll still notifies the operator and surfaces the error.
func TestEngine_TriggerKillSwitch_NotifiesEvenWhenCloseAllFails(t *testing.T) {
	rec := &orderRecorder{closeErr: errors.New("db locked")}
	e := newOrderEngine(t, rec)

	_, err := e.TriggerKillSwitch(context.Background(), domain.KillReasonDailyLossLimit, nil)
	if err == nil {
		t.Fatal("TriggerKillSwitch error = nil, want the CloseAll failure")
	}
	if got := rec.calls; len(got) != 2 || got[0] != "close" || got[1] != "notify" {
		t.Fatalf("calls = %v, want [close notify]", got)
	}
}

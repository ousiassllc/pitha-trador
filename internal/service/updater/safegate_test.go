package updater_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

type fakePositionCounter struct {
	count int
	err   error
}

func (f fakePositionCounter) OpenPositionCount(context.Context) (int, error) { return f.count, f.err }

type fakeSystemStateReader struct {
	state  domain.SystemState
	events []domain.KillSwitchEvent
	err    error
}

func (f fakeSystemStateReader) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return f.state, f.events, f.err
}

type fakeOrderLister struct {
	orders []domain.PaperOrder
	err    error
}

func (f fakeOrderLister) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return f.orders, f.err
}

func newSafeGate(t *testing.T, positions int, state domain.SystemState, lastOrder time.Time, now time.Time) updater.SafeGate {
	t.Helper()
	var orders []domain.PaperOrder
	if !lastOrder.IsZero() {
		orders = []domain.PaperOrder{{SubmittedAt: lastOrder}}
	}
	return updater.SafeGate{
		Positions: fakePositionCounter{count: positions},
		State:     fakeSystemStateReader{state: state},
		Orders:    fakeOrderLister{orders: orders},
		Now:       func() time.Time { return now },
	}
}

func TestSafeGate_SafeToUpdate_AllClear(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gate := newSafeGate(t, 0, domain.SystemStateRunning, time.Time{}, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if !safe {
		t.Fatalf("SafeToUpdate() = false, reason=%+v; want true", reason)
	}
}

func TestSafeGate_SafeToUpdate_BlockedByOpenPositions(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gate := newSafeGate(t, 2, domain.SystemStateRunning, time.Time{}, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if safe {
		t.Fatalf("SafeToUpdate() = true, want false (open positions)")
	}
	if reason.Kind != updater.BlockOpenPositions || reason.Detail == "" {
		t.Fatalf("SafeToUpdate() reason = %+v, want Kind BlockOpenPositions with a detail", reason)
	}
}

func TestSafeGate_SafeToUpdate_BlockedByKillSwitch(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gate := newSafeGate(t, 0, domain.SystemStateKilled, time.Time{}, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if safe {
		t.Fatalf("SafeToUpdate() = true, want false (kill switch active)")
	}
	if reason.Kind != updater.BlockKillSwitch || reason.Detail == "" {
		t.Fatalf("SafeToUpdate() reason = %+v, want Kind BlockKillSwitch with a detail", reason)
	}
}

// Paused (not Killed) must not block an update: issue #65 explicitly
// only names Kill Switch as a gate condition, not the separate manual
// pause flag.
func TestSafeGate_SafeToUpdate_PausedDoesNotBlock(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gate := newSafeGate(t, 0, domain.SystemStatePaused, time.Time{}, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if !safe {
		t.Fatalf("SafeToUpdate() = false, reason=%+v; want true (paused alone is not a gate condition)", reason)
	}
}

func TestSafeGate_SafeToUpdate_BlockedByRecentOrder(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastOrder := now.Add(-1 * time.Minute) // well within the 5-minute default
	gate := newSafeGate(t, 0, domain.SystemStateRunning, lastOrder, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if safe {
		t.Fatalf("SafeToUpdate() = true, want false (order 1m ago, default min idle is 5m)")
	}
	if reason.Kind != updater.BlockRecentOrder || reason.Detail == "" {
		t.Fatalf("SafeToUpdate() reason = %+v, want Kind BlockRecentOrder with a detail", reason)
	}
}

func TestSafeGate_SafeToUpdate_AllowsAfterMinIdleElapsed(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastOrder := now.Add(-10 * time.Minute) // past the 5-minute default
	gate := newSafeGate(t, 0, domain.SystemStateRunning, lastOrder, now)

	safe, reason := gate.SafeToUpdate(context.Background())
	if !safe {
		t.Fatalf("SafeToUpdate() = false, reason=%+v; want true (order 10m ago, past 5m default)", reason)
	}
}

func TestSafeGate_SafeToUpdate_CustomMinIdleAfterOrder(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastOrder := now.Add(-2 * time.Minute)
	gate := newSafeGate(t, 0, domain.SystemStateRunning, lastOrder, now)
	gate.MinIdleAfterOrder = 1 * time.Minute // override: 2m elapsed clears a 1m gate

	safe, reason := gate.SafeToUpdate(context.Background())
	if !safe {
		t.Fatalf("SafeToUpdate() = false, reason=%+v; want true (2m elapsed, custom 1m gate)", reason)
	}
}

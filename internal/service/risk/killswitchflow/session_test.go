package killswitchflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestEngine_SessionGate_HealthAndHeartbeatChecksSkipOffHours(t *testing.T) {
	clock := jstTime(2026, 10, 3, 10, 0) // Saturday
	e, ks := newSessionEngine(t, &clock, fakeHealth{healthy: false})
	ctx := context.Background()

	for _, at := range []time.Time{
		jstTime(2026, 10, 3, 10, 0), // Saturday midday
		jstTime(2026, 9, 29, 22, 0), // weekday night
		jstTime(2026, 9, 29, 12, 0), // lunch break
	} {
		clock = at
		if err := e.RunPeriodicChecks(ctx); err != nil {
			t.Fatalf("RunPeriodicChecks at %v: %v", at, err)
		}
		if err := e.CheckHeartbeatTimeout(ctx); err != nil {
			t.Fatalf("CheckHeartbeatTimeout at %v: %v", at, err)
		}
		if got := unresolvedReasons(t, ks); len(got) != 0 {
			t.Fatalf("kill switch raised off-hours at %v: %v", at, got)
		}
	}

	clock = jstTime(2026, 9, 29, 10, 0)
	if err := e.CheckMarketDataHealth(ctx); err != nil {
		t.Fatalf("CheckMarketDataHealth in session: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonMarketDataDown {
		t.Fatalf("in-session unhealthy market data: unresolved = %v, want [market_data_down]", got)
	}
}

func TestEngine_SessionGate_HeartbeatSilenceCountedFromSessionOpen(t *testing.T) {
	// Operator last used the UI Friday afternoon; nothing overnight/weekend.
	clock := jstTime(2026, 9, 25, 14, 0)
	e, ks := newSessionEngine(t, &clock, fakeHealth{healthy: true})
	ctx := context.Background()
	if err := e.RecordHeartbeat(ctx, clock); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	// Monday 09:30: 65+ hours since the last heartbeat, but only 30
	// in-session minutes elapsed: no timeout.
	clock = jstTime(2026, 9, 28, 9, 30)
	if err := e.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 {
		t.Fatalf("timeout raised at market open: %v", got)
	}

	// Monday 10:59: 119 minutes after the open: still within the timeout.
	clock = jstTime(2026, 9, 28, 10, 59)
	if err := e.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 {
		t.Fatalf("timeout raised at 119 in-session minutes: %v", got)
	}

	// Monday 11:01 (during the 前場): 121 minutes of session silence.
	clock = jstTime(2026, 9, 28, 11, 1)
	if err := e.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonOperatorHeartbeatTimeout {
		t.Fatalf("unresolved = %v, want [operator_heartbeat_timeout]", got)
	}
}

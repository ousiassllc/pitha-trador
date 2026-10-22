package evalflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// A call cut off by Close is neither a failure nor a cost: the day's
// jev.shadow.failures does not move and nothing is stored, so a day of
// restarts cannot reach DailyFailureCap on its own.
func TestShadow_CloseCancellationIsNotCountedAsAFailure(t *testing.T) {
	hold := make(chan struct{})
	e := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{holdShadow: hold})
	t.Cleanup(func() { close(hold) }) // runs before the server's Close: unblocks the held handler
	e.enable()
	e.evaluate()
	deadline := time.Now().Add(10 * time.Second)
	for e.api.calls("trader-v4") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the candidate call never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.trader.Close() // returns once the cancelled call has finished
	cfg := jevshadowsettings.Load(context.Background(), e.settings)
	if got := cfg.Failures.FailuresOn("2026-10-22"); got != 0 {
		t.Fatalf("failure counter after Close = %d, want 0 (a cancelled call is not a failure)", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`); n != 0 {
		t.Fatalf("the cancelled call stored %d shadow rows", n)
	}
}

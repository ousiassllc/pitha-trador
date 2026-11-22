package event_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/guard"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// A panic in the feed loop is contained (this test binary lives) and the feed
// subscribes again: same symbols, resuming after the last event number.
func TestFeedPanicIsContainedAndResubscribes(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"7203", "6758"})
	r.run()
	c1 := r.fb.WaitEvent(t, 1)
	_ = c1.Send(tt.SS(1, 42, true))
	tt.Eventually(t, func() bool { return r.feed.Statuses().SystemKnown })

	r.pc.Arm(1)
	c1.Drop() // the loop looks the session up again, and that panics
	r.advanceUntil(func() bool { return r.pc.Fired() == 1 && len(r.fb.Events()) >= 2 })

	c2 := r.fb.Events()[1]
	if got := c2.Query.Get("p_issue_code"); got != "7203,6758" || c2.Query.Get("p_eno") != "42" {
		t.Errorf("resubscribed with symbols %q p_eno %q, want 7203,6758 after 42", got, c2.Query.Get("p_eno"))
	}
	tt.Eventually(t, func() bool { return fmt.Sprint(r.feed.Subscribed()) == "[7203 6758]" })
	if r.stops.Load() != 0 {
		t.Error("one panic must not stop the feed")
	}
}

// The feed loop is started again up to guard.MaxRestarts times, then
// gives up: Run returns, OnStop runs once, nothing is subscribed or dialed.
func TestFeedPanicRetriesUpToTheLimitThenStops(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"7203"})
	r.run()
	c1 := r.fb.WaitEvent(t, 1)

	r.pc.Arm(guard.MaxRestarts + 1)
	c1.Drop()
	last := guard.MaxRestarts + 1
	r.advanceUntil(func() bool { return r.pc.Fired() == last })
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Feed.Run did not return after giving up")
	}
	if r.stops.Load() != 1 {
		t.Errorf("OnStop calls = %d, want 1", r.stops.Load())
	}
	if len(r.feed.Subscribed()) != 0 {
		t.Errorf("Subscribed = %v after giving up", r.feed.Subscribed())
	}
	r.clk.Advance(2 * time.Hour)
	if got := len(r.fb.Events()); got != 1 {
		t.Errorf("EVENT connections = %d, a stopped feed must not dial again", got)
	}
}

package gmo

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestCallsAreSpacedAtLeastOneSecond(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"}, step{fixture: "ticker.json"}, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{})

	for range 3 {
		okTicker(t, c)
	}

	arr := srv.arrivals()
	for i := 1; i < len(arr); i++ {
		if gap := arr[i].Sub(arr[i-1]); gap != time.Second {
			t.Fatalf("calls %d and %d are %s apart, want exactly 1s", i, i+1, gap)
		}
	}
	if got, want := clk.recordedSleeps(), []time.Duration{time.Second, time.Second}; !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v (the first call does not wait)", got, want)
	}
}

func TestOnlyTheRemainingTimeIsWaited(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"}, step{fixture: "ticker.json"}, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{})

	okTicker(t, c)
	clk.advance(400 * time.Millisecond) // caller was busy
	okTicker(t, c)
	clk.advance(5 * time.Second) // idle for longer than the interval
	okTicker(t, c)

	if got, want := clk.recordedSleeps(), []time.Duration{600 * time.Millisecond}; !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
}

func TestConcurrentCallersShareOneRateLimit(t *testing.T) {
	const callers = 8
	clk := newFakeClock()
	steps := make([]step, callers)
	for i := range steps {
		steps[i] = step{fixture: "ticker.json"}
	}
	srv := newFakeServer(t, clk, steps...)
	c, _ := testClient(t, srv, clk, Config{})

	start := clk.Now()
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			_, err := c.Ticker(context.Background(), "USD_JPY")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	want := slices.Repeat([]time.Duration{time.Second}, callers-1)
	if got := clk.recordedSleeps(); !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %d sleeps of 1s", got, callers-1)
	}
	if elapsed := clk.Now().Sub(start); elapsed != time.Duration(callers-1)*time.Second {
		t.Fatalf("%d concurrent calls took %s of virtual time, want %ds", callers, elapsed, callers-1)
	}
	if n := len(srv.requests()); n != callers {
		t.Fatalf("%d server calls, want %d", n, callers)
	}
}

func TestConfiguredMinIntervalIsUsed(t *testing.T) {
	clk := newFakeClock()
	srv := newFakeServer(t, clk, step{fixture: "ticker.json"}, step{fixture: "ticker.json"})
	c, _ := testClient(t, srv, clk, Config{MinInterval: 3 * time.Second})

	okTicker(t, c)
	okTicker(t, c)

	if got, want := clk.recordedSleeps(), []time.Duration{3 * time.Second}; !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
}

func TestLimiterWaitStopsWhenContextIsDone(t *testing.T) {
	l := newLimiter(newFakeClock(), time.Second)
	l.sem <- struct{}{} // another caller holds the slot
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want context.Canceled", err)
	}
}

func TestLimiterCancelledSleepDoesNotConsumeTheSlot(t *testing.T) {
	clk := newFakeClock()
	l := newLimiter(clk, time.Second)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want context.Canceled", err)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("limiter stuck after a cancelled wait: %v", err)
	}
}

// wait must fail on a cancelled ctx even though the slot is free, without
// taking the slot, sleeping or counting as a call. Note: this single-call
// check is deterministic but cannot by itself catch the pre-fix bug (a
// select with both cases ready picks one at random, so the old code failed
// only some of the time); the other cancellation tests cover the blocked-slot
// path.
func TestLimiterWaitWithCancelledContextFailsWithoutSideEffects(t *testing.T) {
	clk := newFakeClock()
	l := newLimiter(clk, time.Second)
	start := clk.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want context.Canceled", err)
	}
	if len(l.sem) != 0 {
		t.Fatal("a cancelled wait must not take the slot")
	}
	if !l.last.IsZero() {
		t.Fatal("a cancelled wait must not count as a call")
	}
	if s := clk.recordedSleeps(); len(s) != 0 || !clk.Now().Equal(start) {
		t.Fatalf("sleeps = %v, clock moved to %s; want neither", s, clk.Now())
	}
}

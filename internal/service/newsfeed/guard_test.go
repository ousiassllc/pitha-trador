package newsfeed_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// scriptedFeed fails while failing is set and counts calls.
type scriptedFeed struct {
	mu      sync.Mutex
	calls   int
	failing bool
}

func (f *scriptedFeed) Fetch(context.Context, string) ([]assist.NewsItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failing {
		return nil, errors.New("feed down")
	}
	return []assist.NewsItem{{ID: "a", Headline: "h"}}, nil
}

func (f *scriptedFeed) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

func newGuard(feed newsfeed.Feed, c *clock) *newsfeed.GuardedFeed {
	return newsfeed.NewGuardedFeed(feed, newsfeed.WithBackoff(time.Minute, 8*time.Minute), newsfeed.WithGuardClock(c.Now))
}

func TestGuardedFeed_FailureSkipsRequestsUntilTheBackoffElapses(t *testing.T) {
	inner, c := &scriptedFeed{failing: true}, &clock{now: t0}
	guard := newGuard(inner, c)
	ctx := context.Background()

	if _, err := guard.Fetch(ctx, "7203"); err == nil || errors.Is(err, newsfeed.ErrBackingOff) {
		t.Fatalf("first Fetch err = %v, want the inner error", err)
	}
	c.advance(30 * time.Second)
	if _, err := guard.Fetch(ctx, "6758"); !errors.Is(err, newsfeed.ErrBackingOff) {
		t.Fatalf("Fetch during backoff err = %v, want ErrBackingOff (any symbol)", err)
	}
	if inner.callCount() != 1 {
		t.Errorf("inner calls = %d, want 1: no request while backing off", inner.callCount())
	}
	c.advance(31 * time.Second)
	if _, err := guard.Fetch(ctx, "7203"); errors.Is(err, newsfeed.ErrBackingOff) || inner.callCount() != 2 {
		t.Errorf("after the backoff: err = %v, calls = %d, want a real request", err, inner.callCount())
	}
}

func TestGuardedFeed_ConsecutiveFailuresLowerTheQueryFrequencyUpToTheCap(t *testing.T) {
	inner, c := &scriptedFeed{failing: true}, &clock{now: t0}
	guard := newGuard(inner, c)
	ctx := context.Background()

	// Cool-downs after consecutive failed incidents: 1m, 2m, 4m, 8m, 8m.
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 8 * time.Minute} {
		if _, err := guard.Fetch(ctx, "7203"); err == nil || errors.Is(err, newsfeed.ErrBackingOff) {
			t.Fatalf("failure %d: err = %v, want the inner error", i+1, err)
		}
		c.advance(want - time.Second)
		if _, err := guard.Fetch(ctx, "7203"); !errors.Is(err, newsfeed.ErrBackingOff) {
			t.Fatalf("failure %d: still within %v but err = %v, want ErrBackingOff", i+1, want, err)
		}
		c.advance(time.Second)
	}
}

func TestGuardedFeed_SuccessResetsTheBackoff(t *testing.T) {
	inner, c := &scriptedFeed{failing: true}, &clock{now: t0}
	guard := newGuard(inner, c)
	ctx := context.Background()

	_, _ = guard.Fetch(ctx, "7203") // 1m cool-down
	c.advance(time.Minute)
	_, _ = guard.Fetch(ctx, "7203") // 2m cool-down
	c.advance(2 * time.Minute)
	inner.failing = false
	if _, err := guard.Fetch(ctx, "7203"); err != nil {
		t.Fatalf("Fetch after recovery: %v", err)
	}
	inner.failing = true
	_, _ = guard.Fetch(ctx, "7203") // back to the 1m base, not 4m
	c.advance(time.Minute)
	if _, err := guard.Fetch(ctx, "7203"); errors.Is(err, newsfeed.ErrBackingOff) {
		t.Error("cool-down after recovery did not restart from the base delay")
	}
}

func TestGuardedFeed_AFailedBatchCountsAsOneIncident(t *testing.T) {
	inner, c := &scriptedFeed{failing: true}, &clock{now: t0}
	guard := newGuard(inner, c)
	ctx := context.Background()

	// Four symbols of one poll cycle fetch concurrently and all fail: the
	// cool-down must still be the base delay, not 2^4 times it.
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, symbol := range []string{"1", "2", "3", "4"} {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, _ = guard.Fetch(ctx, symbol) }()
	}
	close(start)
	wg.Wait()

	c.advance(time.Minute + time.Second)
	if _, err := guard.Fetch(ctx, "7203"); errors.Is(err, newsfeed.ErrBackingOff) {
		t.Error("a concurrent failed batch lengthened the cool-down beyond the base delay")
	}
}

func TestGuardedFeed_CancelledContextIsNotAFeedFailure(t *testing.T) {
	inner, c := &scriptedFeed{failing: true}, &clock{now: t0}
	guard := newGuard(inner, c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = guard.Fetch(ctx, "7203")
	if _, err := guard.Fetch(context.Background(), "7203"); errors.Is(err, newsfeed.ErrBackingOff) {
		t.Error("shutdown (cancelled context) started a backoff")
	}
}

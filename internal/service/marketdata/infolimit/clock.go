package infolimit

import (
	"context"
	"sync"
	"time"
)

// Clock is the time source the limiter waits on. Tests inject ManualClock
// so Wait/Sleep do not block on the wall clock.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// ManualClock is a deterministic Clock for tests. After auto-advances Now
// by the requested duration and returns a ready channel.
type ManualClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewManualClock returns a ManualClock starting at now. A zero now uses
// 2026-10-05T06:06:17Z (the production 4001006 burst that motivated #514).
func NewManualClock(now time.Time) *ManualClock {
	if now.IsZero() {
		now = time.Date(2026, 10, 5, 6, 6, 17, 0, time.UTC)
	}
	return &ManualClock{now: now}
}

// Now returns the fake current time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After auto-advances Now by d and returns a channel that is already ready.
func (c *ManualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return ch
}

// Sleep waits d on clock, or until ctx is done.
func Sleep(ctx context.Context, clock Clock, d time.Duration) error {
	if clock == nil {
		clock = realClock{}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-clock.After(d):
		return nil
	}
}

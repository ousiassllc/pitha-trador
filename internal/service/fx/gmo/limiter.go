package gmo

import (
	"context"
	"time"
)

// limiter spaces calls at least interval apart, measured from the moment the
// previous call was released, for every goroutine sharing it. The one-slot
// semaphore serialises waiters (and keeps cancelling a waiter possible);
// last is only touched while the slot is held.
type limiter struct {
	clock    Clock
	interval time.Duration
	sem      chan struct{}
	last     time.Time
}

func newLimiter(clock Clock, interval time.Duration) *limiter {
	return &limiter{clock: clock, interval: interval, sem: make(chan struct{}, 1)}
}

// wait blocks until a call may start, or returns ctx's error.
func (l *limiter) wait(ctx context.Context) error {
	// select picks randomly when both cases are ready; check first so a
	// cancelled ctx is always an error.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-l.sem }()

	if !l.last.IsZero() {
		if d := l.interval - l.clock.Now().Sub(l.last); d > 0 {
			if err := l.clock.Sleep(ctx, d); err != nil {
				return err
			}
		}
	}
	l.last = l.clock.Now()
	return nil
}

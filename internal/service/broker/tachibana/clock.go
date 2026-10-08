package tachibana

import (
	"context"
	"time"
)

// Clock is the adapter's time source. Production uses RealClock; tests inject
// a fake so that "03:30 close → 05:35 re-login" and the request rate window
// run without real waiting.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is the part of *time.Timer the adapter uses.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// RealClock is the wall clock.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now() }

// NewTimer implements Clock.
func (RealClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }

func (r realTimer) Stop() bool { return r.t.Stop() }

// Sleep waits d on clk or until ctx is done, whichever is first.
func Sleep(ctx context.Context, clk Clock, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := clk.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C():
		return nil
	}
}

// OrReal is clk, or RealClock when clk is nil.
func OrReal(clk Clock) Clock {
	if clk == nil {
		return RealClock{}
	}
	return clk
}

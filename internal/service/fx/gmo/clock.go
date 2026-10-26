package gmo

import (
	"context"
	"time"
)

// Clock is the Client's time source. Production uses the wall clock; tests
// inject a fake so the 1-call-per-second spacing and the retry backoff run
// without real waiting.
type Clock interface {
	Now() time.Time
	// Sleep waits d or until ctx is done (returning ctx.Err()).
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

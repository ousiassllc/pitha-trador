package infolimit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// OfficialMaxPerSecond is the kabuステーションAPI FAQ cap for
	// 情報API / 取引余力API / 銘柄登録API (https://kabucom.github.io/kabusapi/ptal/faq.html).
	OfficialMaxPerSecond = 10
	// DefaultMaxPerSecond is the process-wide default (a margin under the official 10).
	DefaultMaxPerSecond = 8
)

// Stats is a snapshot of limiter activity for logs, tests, and the scan panel.
type Stats struct {
	LimitPerSecond int
	Overflows      int64
	LastOverflowAt time.Time
}

// Limiter is a sliding 1-second window shared by every information/register
// REST call in the process.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	clock  Clock
	times  []time.Time

	overflows        atomic.Int64
	lastOverflowUnix atomic.Int64
	lastSaturatedLog atomic.Int64
}

// New returns a Limiter of maxPerSecond grants per rolling second.
// maxPerSecond <= 0 becomes DefaultMaxPerSecond; values above
// OfficialMaxPerSecond are clamped. A nil clock uses the wall clock.
func New(maxPerSecond int, clock Clock) *Limiter {
	if maxPerSecond <= 0 {
		maxPerSecond = DefaultMaxPerSecond
	}
	if maxPerSecond > OfficialMaxPerSecond {
		maxPerSecond = OfficialMaxPerSecond
	}
	if clock == nil {
		clock = realClock{}
	}
	return &Limiter{limit: maxPerSecond, window: time.Second, clock: clock}
}

// Wait blocks until a grant is available or ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait, ok := l.tryGrant()
		if ok {
			return nil
		}
		l.noteSaturated(wait)
		if err := Sleep(ctx, l.clock, wait); err != nil {
			return err
		}
	}
}

func (l *Limiter) tryGrant() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(l.times) && !l.times[i].After(cutoff) {
		i++
	}
	l.times = l.times[i:]
	if len(l.times) < l.limit {
		l.times = append(l.times, now)
		return 0, true
	}
	wait := l.times[0].Add(l.window).Sub(now)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return wait, false
}

func (l *Limiter) noteSaturated(wait time.Duration) {
	now := l.clock.Now().UnixNano()
	prev := l.lastSaturatedLog.Load()
	if prev != 0 && now-prev < int64(time.Second) {
		return
	}
	if !l.lastSaturatedLog.CompareAndSwap(prev, now) {
		return
	}
	slog.Warn("marketdata: kabu info api rate limiter saturated",
		"limit_per_sec", l.limit, "wait_ms", wait.Milliseconds())
}

// NoteOverflow records a 429 / 4001006 the limiter did not prevent.
func (l *Limiter) NoteOverflow() {
	l.overflows.Add(1)
	l.lastOverflowUnix.Store(l.clock.Now().UnixNano())
}

// Stats returns a snapshot of limiter activity.
func (l *Limiter) Stats() Stats {
	var last time.Time
	if n := l.lastOverflowUnix.Load(); n > 0 {
		last = time.Unix(0, n).UTC()
	}
	return Stats{LimitPerSecond: l.limit, Overflows: l.overflows.Load(), LastOverflowAt: last}
}

// Clock returns the limiter's clock (for rate-limit backoff Sleep).
func (l *Limiter) Clock() Clock { return l.clock }

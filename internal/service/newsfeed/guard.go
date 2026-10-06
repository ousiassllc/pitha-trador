package newsfeed

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

const (
	// DefaultBackoffBase is the first cool-down after a failed fetch. The
	// poll interval is one minute, so the first failure already skips the
	// next cycle.
	DefaultBackoffBase = 2 * time.Minute
	// DefaultBackoffMax caps the cool-down so a recovered feed is noticed
	// within half an hour.
	DefaultBackoffMax = 30 * time.Minute
)

// ErrBackingOff is returned by GuardedFeed.Fetch, without any request being
// made, while the feed is cooling down after consecutive failures.
var ErrBackingOff = errors.New("newsfeed: feed is backing off after consecutive failures")

// GuardedFeed wraps a Feed with the fail-safe the unofficial やのしん
// service needs (FR-LUNA-4, issue #273): after a failed fetch every request
// is skipped (ErrBackingOff) for an exponentially growing cool-down, so a
// stopped or slow feed is queried less and less often instead of every
// minute for every symbol. One success resets it. It never makes a failure
// worse than the inner Feed's error and never panics or blocks beyond the
// inner Feed's own timeout.
type GuardedFeed struct {
	inner Feed
	base  time.Duration
	ceil  time.Duration
	now   func() time.Time

	mu           sync.Mutex
	failures     int       // consecutive failed incidents
	armedAt      time.Time // when the current cool-down started
	backoffUntil time.Time
}

// GuardOption configures a GuardedFeed.
type GuardOption func(*GuardedFeed)

// WithBackoff overrides DefaultBackoffBase and DefaultBackoffMax.
func WithBackoff(base, ceiling time.Duration) GuardOption {
	return func(g *GuardedFeed) {
		if base > 0 && ceiling >= base {
			g.base, g.ceil = base, ceiling
		}
	}
}

// WithGuardClock overrides time.Now (tests).
func WithGuardClock(now func() time.Time) GuardOption {
	return func(g *GuardedFeed) { g.now = now }
}

// NewGuardedFeed returns inner wrapped with the failure backoff.
func NewGuardedFeed(inner Feed, opts ...GuardOption) *GuardedFeed {
	g := &GuardedFeed{inner: inner, base: DefaultBackoffBase, ceil: DefaultBackoffMax, now: time.Now}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Fetch implements Feed.
func (g *GuardedFeed) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	started := g.now()
	if g.backingOff(started) {
		return nil, ErrBackingOff
	}
	items, err := g.inner.Fetch(ctx, symbol)
	g.record(ctx, started, err)
	return items, err
}

func (g *GuardedFeed) backingOff(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return now.Before(g.backoffUntil)
}

// record updates the failure state after a call that started at started.
// A cancelled context (shutdown) says nothing about the feed. Requests of
// one poll cycle run concurrently, so a failure of a call that started no
// later than the moment the current cool-down was armed belongs to the same
// incident and does not lengthen it (a later call would have been skipped).
func (g *GuardedFeed) record(ctx context.Context, started time.Time, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case err == nil:
		g.failures, g.armedAt, g.backoffUntil = 0, time.Time{}, time.Time{}
	case ctx.Err() != nil:
	case !g.armedAt.IsZero() && !started.After(g.armedAt):
	default:
		g.failures++
		g.armedAt = g.now()
		delay := g.delay()
		g.backoffUntil = g.armedAt.Add(delay)
		slog.Warn("newsfeed: feed failing, lowering the query frequency", "consecutive_failures", g.failures, "backoff", delay.String(), "error", err)
	}
}

// delay is base * 2^(failures-1), capped at ceil.
func (g *GuardedFeed) delay() time.Duration {
	d := g.base
	for i := 1; i < g.failures && d < g.ceil; i++ {
		d *= 2
	}
	return min(d, g.ceil)
}

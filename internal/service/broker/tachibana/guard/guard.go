// Package guard is the panic restart policy of the 立花 adapter's resident
// goroutines (FR-SCHED-6, issue #1211).
package guard

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// A panic is logged, the goroutine is
// started again after a doubling delay, and once MaxRestarts restarts
// have panicked in a row the 立花 connection is stopped while the rest of the
// process keeps running.
const (
	// MaxRestarts is how many times a panicked goroutine is started again
	// before the adapter gives up (the goroutine runs at most 1+5 times).
	MaxRestarts = 5
	// BackoffMin is the delay before the first restart; it doubles
	// per restart up to BackoffMax (5s, 10s, 20s, 40s, 60s).
	BackoffMin = 5 * time.Second
	BackoffMax = time.Minute
	// HealthyAfter: a run that lasted this long before panicking was
	// healthy, so the restart count and delay start over.
	HealthyAfter = time.Hour
)

// Supervise runs fn until it returns without panicking. A panic is logged with
// its stack (safego), then fn is run again after the restart delay, preceded by
// restart (the place to reset state a panic may have left half-done). After
// MaxRestarts consecutive panicking runs Supervise returns true and the
// caller stops what fn kept alive. It returns false when fn returned normally
// or ctx ended.
func Supervise(ctx context.Context, clk tachibana.Clock, what string, fn, restart func()) (gaveUp bool) {
	restarts, backoff := 0, BackoffMin
	for {
		started := clk.Now()
		if !safego.Run(what, fn) || ctx.Err() != nil {
			return false
		}
		if clk.Now().Sub(started) >= HealthyAfter {
			restarts, backoff = 0, BackoffMin
		}
		if restarts >= MaxRestarts {
			slog.Error("tachibana: resident goroutine keeps panicking; giving up", "task", what, "restarts", restarts)
			return true
		}
		restarts++
		slog.Warn("tachibana: resident goroutine panicked; restarting", "task", what,
			"restart", restarts, "max_restarts", MaxRestarts, "retry_in", backoff.String())
		if tachibana.Sleep(ctx, clk, backoff) != nil {
			return false
		}
		backoff = min(backoff*2, BackoffMax)
		restart()
	}
}

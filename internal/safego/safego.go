// Package safego is FR-SCHED-6's panic guard for resident goroutines: a
// panic is logged with its stack trace through slog instead of terminating
// the process. Loops guard each cycle with Run so one panicking cycle is
// logged and the next cycle still runs (the "restart").
package safego

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// Recover must be deferred directly (`defer safego.Recover("x")`): it
// recovers a panic in the deferring goroutine and logs it with the stack.
func Recover(what string) {
	if r := recover(); r != nil {
		logPanic(what, r)
	}
}

// Run calls fn and reports whether it panicked. A panic is logged (with
// what and the stack) and swallowed, so a loop can call Run once per cycle
// and carry on after a bad one.
func Run(what string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(what, r)
			panicked = true
		}
	}()
	fn()
	return false
}

// Try is Run for a function returning an error: a panic is logged and
// returned as an error so callers can treat it like any failed attempt.
func Try(what string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(what, r)
			err = fmt.Errorf("%s: panic: %v", what, r)
		}
	}()
	return fn()
}

// Loop calls cycle once after every wait() until ctx is done. Each cycle
// is guarded: a panic is logged with its stack and an error is logged, and
// the loop carries on with the next cycle.
func Loop(ctx context.Context, what string, wait func() time.Duration, cycle func(context.Context) error) {
	for {
		timer := time.NewTimer(wait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			err := Try(what, func() error { return cycle(ctx) })
			if err != nil && ctx.Err() == nil {
				slog.Error(what+" cycle failed", "error", err)
			}
		}
	}
}

func logPanic(what string, r any) {
	slog.Error("background task panicked", "task", what, "panic", r, "stack", string(debug.Stack()))
}

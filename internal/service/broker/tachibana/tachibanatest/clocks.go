// Package tachibanatest is the test support of the 立花 adapter packages: fake
// clocks and a fake e支店 server that speaks the REQUEST I/F (Shift-JIS JSON
// over HTTPS POST, RSA-OAEP encrypted virtual URLs).
package tachibanatest

import (
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// AtJST builds a JST instant (seconds optional).
func AtJST(y int, m time.Month, d, hh, mm int, ss ...int) time.Time {
	sec := 0
	if len(ss) > 0 {
		sec = ss[0]
	}
	return time.Date(y, m, d, hh, mm, sec, 0, tachibana.JST)
}

// Eventually polls cond until it holds, failing the test after a few seconds.
func Eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ManualClock is advanced by the test; timers fire when it passes them.
type ManualClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*manualTimer
	created int
}

type manualTimer struct {
	clk  *ManualClock
	c    chan time.Time
	at   time.Time
	done bool
}

// NewManualClock returns a ManualClock at now.
func NewManualClock(now time.Time) *ManualClock { return &ManualClock{now: now} }

// Now implements tachibana.Clock.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer implements tachibana.Clock.
func (c *ManualClock) NewTimer(d time.Duration) tachibana.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created++
	t := &manualTimer{clk: c, c: make(chan time.Time, 1), at: c.now.Add(d)}
	if d <= 0 {
		t.done = true
		t.c <- c.now
		return t
	}
	c.timers = append(c.timers, t)
	return t
}

func (t *manualTimer) C() <-chan time.Time { return t.c }

func (t *manualTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	was := !t.done
	t.done = true
	return was
}

// Advance moves the clock forward by d, firing every timer it passes.
func (c *ManualClock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

// Set moves the clock to t (never backwards), firing due timers.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.now) {
		c.now = t
	}
	for _, tm := range c.timers {
		if !tm.done && !tm.at.After(c.now) {
			tm.done = true
			tm.c <- c.now
		}
	}
}

// Created is how many timers were ever created.
func (c *ManualClock) Created() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.created
}

// WaitCreated blocks until more than n timers exist (the loop re-armed).
func (c *ManualClock) WaitCreated(t *testing.T, n int) {
	t.Helper()
	Eventually(t, func() bool { return c.Created() > n })
}

// AutoClock never blocks: creating a timer advances virtual time by its
// duration and fires it at once.
type AutoClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewAutoClock returns an AutoClock at now.
func NewAutoClock(now time.Time) *AutoClock { return &AutoClock{now: now} }

// Now implements tachibana.Clock.
func (c *AutoClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// SetNow jumps the clock to t.
func (c *AutoClock) SetNow(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// NewTimer implements tachibana.Clock.
func (c *AutoClock) NewTimer(d time.Duration) tachibana.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.now = c.now.Add(d)
	}
	t := &autoTimer{c: make(chan time.Time, 1)}
	t.c <- c.now
	return t
}

type autoTimer struct{ c chan time.Time }

func (t *autoTimer) C() <-chan time.Time { return t.c }
func (t *autoTimer) Stop() bool          { return false }

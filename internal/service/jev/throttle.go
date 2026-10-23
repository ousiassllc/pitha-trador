package jev

import (
	"sync"
	"time"
)

// callKind is the standing of a Jev call on the shared API key (issue #883).
type callKind int

const (
	// liveCall is Scout/Trader: it feeds the rolling error rate behind Healthy
	// (the jev_api_down Kill Switch) and waits out a 429/529 with backoff.
	liveCall callKind = iota
	// auxiliaryCall is Ask (Luna/Sol/Opus): retried like a live call but never
	// feeds the error rate.
	auxiliaryCall
	// shadowCall is the shadow comparison (ShadowTrader, issue #872): it never
	// feeds the error rate, is not retried after a 429/529 (the shadow stops
	// instead of waiting, so it never adds a retry storm to the key the live
	// calls use) and its own 429/529 is not noted for LastThrottle.
	shadowCall
)

// ThrottleEvent is the last 429 (rate limited) or 529 (overloaded) a call that is
// not a shadow call received from the API.
type ThrottleEvent struct {
	// Status is 429 or 529.
	Status int
	// At is when the answer arrived (Config.Now).
	At time.Time
}

// throttleNote holds the last ThrottleEvent; its zero value is "none yet".
type throttleNote struct {
	mu   sync.Mutex
	last ThrottleEvent
}

// noteThrottle remembers err when it is a 429/529 answer. Scout, Trader and Ask
// call it for every failed attempt; the shadow reads it (LastThrottle) to leave
// the API alone while the live calls are being throttled.
func (c *Client) noteThrottle(err error) {
	status := throttleStatus(err)
	if status == 0 {
		return
	}
	c.throttle.mu.Lock()
	c.throttle.last = ThrottleEvent{Status: status, At: c.now()}
	c.throttle.mu.Unlock()
}

// LastThrottle returns the last 429/529 that a Scout, Trader or Ask call
// received, and false when there has been none since the Client was built.
// ShadowTrader calls never set it.
func (c *Client) LastThrottle() (ThrottleEvent, bool) {
	c.throttle.mu.Lock()
	defer c.throttle.mu.Unlock()
	return c.throttle.last, c.throttle.last.Status != 0
}

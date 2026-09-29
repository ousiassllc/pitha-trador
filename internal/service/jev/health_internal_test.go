package jev

import (
	"testing"
	"time"
)

// A breach with no call for breachStaleAfter no longer counts as current:
// once the jev_api_down Kill Switch stops new Jev calls, the window can
// never show recovery on its own, so auto-resume needs this expiry.
func TestErrorRateTracker_BreachExpiresWithoutCalls(t *testing.T) {
	tracker := newErrorRateTracker(5)
	for range 5 {
		tracker.record(true, 0.5)
	}
	if !tracker.isBreached() {
		t.Fatal("isBreached() = false after 5 failures, want true")
	}

	tracker.mu.Lock()
	tracker.lastCall = time.Now().Add(-breachStaleAfter - time.Second)
	tracker.mu.Unlock()
	if tracker.isBreached() {
		t.Fatal("isBreached() = true with no call for breachStaleAfter, want false")
	}

	tracker.record(true, 0.5)
	if !tracker.isBreached() {
		t.Fatal("isBreached() = false after a new failed call, want true")
	}
}

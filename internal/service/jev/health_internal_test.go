package jev

import (
	"testing"
	"time"
)

// A breach with no call for breachStaleAfter no longer counts as current:
// once the jev_api_down Kill Switch stops new Jev calls, the window can
// never show recovery on its own, so auto-resume needs this expiry. The
// first call after expiry starts a fresh window: a success must not
// re-breach on the stale failures; only minErrorRateSamples new calls
// with enough failures re-breach (and re-alert).
func TestErrorRateTracker_BreachExpiresWithoutCalls(t *testing.T) {
	tracker := newErrorRateTracker(20)
	for range minErrorRateSamples {
		tracker.record(true, 0.5)
	}
	tracker.mu.Lock()
	tracker.lastCall = time.Now().Add(-breachStaleAfter - time.Second)
	tracker.mu.Unlock()
	if tracker.isBreached() {
		t.Fatal("isBreached() = true with no call for breachStaleAfter, want false")
	}

	if _, newly := tracker.record(false, 0.5); newly || tracker.isBreached() {
		t.Fatalf("success after expiry: newlyBreached=%v isBreached=%v, want false/false", newly, tracker.isBreached())
	}
	var newly bool
	for range minErrorRateSamples - 2 {
		_, newly = tracker.record(true, 0.5)
	}
	if newly || tracker.isBreached() {
		t.Fatal("breached below minErrorRateSamples fresh calls, want not breached")
	}
	if _, newly = tracker.record(true, 0.5); !newly || !tracker.isBreached() {
		t.Fatalf("after %d fresh calls: newlyBreached=%v isBreached=%v, want true/true", minErrorRateSamples, newly, tracker.isBreached())
	}
}

package jev

import (
	"context"
	"sync"
)

// AlertNotifier is notified when Jev API's rolling error rate crosses
// ErrorRateThreshold (non-functional.md §5.2 "Jev APIエラー率上昇（しき
// い値超過）"). A local interface - rather than this package importing
// internal/service/notify directly - keeps it from depending on a
// sibling service/ sub-package (mirrors internal/service/scheduler.
// HeartbeatChecker's own precedent, package doc.go's layer rule);
// internal/service/notify.SlackNotifier implements it directly.
type AlertNotifier interface {
	JevAPIErrorRateExceeded(ctx context.Context, rate, threshold float64) error
}

// NoopAlertNotifier is the placeholder AlertNotifier used until a later
// composition-root step wires internal/service/notify.SlackNotifier in.
type NoopAlertNotifier struct{}

func (NoopAlertNotifier) JevAPIErrorRateExceeded(context.Context, float64, float64) error {
	return nil
}

// minErrorRateSamples is how many calls errorRateTracker requires in its
// window before it starts evaluating rate against a threshold, avoiding
// a spurious "100% error rate" alert from e.g. a single early failure.
const minErrorRateSamples = 5

// errorRateTracker is a fixed-size sliding window of the most recent
// Client.Scout/Trader call outcomes (true = the call ultimately failed,
// after retries), reporting the current rolling error rate and whether a
// call just newly crossed threshold from below. newlyBreached lets the
// caller notify once per breach episode instead of once per call while
// the rate stays elevated. Safe for concurrent use.
type errorRateTracker struct {
	mu       sync.Mutex
	window   []bool
	size     int
	breached bool
}

func newErrorRateTracker(size int) *errorRateTracker {
	return &errorRateTracker{size: size}
}

func (t *errorRateTracker) record(failed bool, threshold float64) (rate float64, newlyBreached bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.window = append(t.window, failed)
	if len(t.window) > t.size {
		t.window = t.window[1:]
	}
	minSamples := t.size
	if minSamples > minErrorRateSamples {
		minSamples = minErrorRateSamples
	}
	if len(t.window) < minSamples {
		return 0, false
	}

	errorCount := 0
	for _, f := range t.window {
		if f {
			errorCount++
		}
	}
	rate = float64(errorCount) / float64(len(t.window))

	isBreach := rate >= threshold
	newlyBreached = isBreach && !t.breached
	t.breached = isBreach
	return rate, newlyBreached
}

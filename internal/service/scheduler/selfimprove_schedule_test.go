package scheduler

import (
	"testing"
	"time"
)

// FR-SELFIMPROVE-1: fires 15:40 JST on weekdays whatever zone the caller's
// time values carry. The schedule pins selfImproveLocation itself, so the test
// varies the input's zone rather than mutating the process-global time.Local.
func TestSelfImproveSchedule_FiresAfterCloseInJSTRegardlessOfHostZone(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	sched, err := selfImproveSchedule()
	if err != nil {
		t.Fatalf("selfImproveSchedule: %v", err)
	}
	fri := time.Date(2026, 10, 2, 15, 40, 0, 0, jst)
	for _, zone := range []*time.Location{time.UTC, jst, time.FixedZone("PST", -8*60*60)} {
		got := sched.Next(fri.Add(-16 * time.Hour).In(zone))
		if next := sched.Next(got); !got.Equal(fri) || !next.Equal(fri.AddDate(0, 0, 3)) { // Fri, then Mon
			t.Errorf("zone %s: Next fires %s then %s, want Fri 15:40 JST then Mon", zone, got.In(jst), next.In(jst))
		}
	}
}

// WithPollSignalForTest makes workers poll only when ch delivers a value.
func WithPollSignalForTest(ch <-chan time.Time) Option {
	return func(s *Scheduler) { s.pollSignal = ch }
}

package scheduler

import (
	"testing"
	"time"
)

// FR-SELFIMPROVE-1: fires 15:40 JST on weekdays whatever the host time.Local.
func TestSelfImproveSchedule_FiresAfterCloseInJSTRegardlessOfHostZone(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	defer func(orig *time.Location) { time.Local = orig }(time.Local)
	for _, host := range []*time.Location{time.UTC, jst, time.FixedZone("PST", -8*60*60)} {
		time.Local = host
		sched, err := selfImproveSchedule()
		if err != nil {
			t.Fatalf("selfImproveSchedule: %v", err)
		}
		fri := time.Date(2026, 10, 2, 15, 40, 0, 0, jst)
		got := sched.Next(fri.Add(-16 * time.Hour))
		if next := sched.Next(got); !got.Equal(fri) || !next.Equal(fri.AddDate(0, 0, 3)) { // Fri, then Mon
			t.Errorf("host %s: Next fires %s then %s, want Fri 15:40 JST then Mon", host, got.In(jst), next.In(jst))
		}
	}
}

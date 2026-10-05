package infolimit

import (
	"context"
	"testing"
	"time"
)

func TestLimiter_CapsGrantsPerRollingSecond(t *testing.T) {
	start := time.Date(2026, 10, 5, 6, 6, 17, 0, time.UTC)
	clock := NewManualClock(start)
	lim := New(8, clock)
	ctx := context.Background()

	var stamps []time.Time
	for range 24 {
		if err := lim.Wait(ctx); err != nil {
			t.Fatalf("Wait: %v", err)
		}
		stamps = append(stamps, clock.Now())
	}
	if got := maxInWindow(stamps, time.Second); got > 8 {
		t.Fatalf("max grants in any 1s window = %d, want <= 8", got)
	}
	if stamps[23].Sub(stamps[0]) < 2*time.Second {
		t.Fatalf("24 grants at 8/s spanned %s, want at least 2s", stamps[23].Sub(stamps[0]))
	}
}

func TestLimiter_ClampsAboveOfficialCap(t *testing.T) {
	lim := New(50, NewManualClock(time.Time{}))
	if got := lim.Stats().LimitPerSecond; got != OfficialMaxPerSecond {
		t.Fatalf("limit = %d, want official cap %d", got, OfficialMaxPerSecond)
	}
}

func TestLimiter_DefaultWhenNonPositive(t *testing.T) {
	lim := New(0, NewManualClock(time.Time{}))
	if got := lim.Stats().LimitPerSecond; got != DefaultMaxPerSecond {
		t.Fatalf("limit = %d, want default %d", got, DefaultMaxPerSecond)
	}
}

func TestLimiter_NoteOverflow(t *testing.T) {
	clock := NewManualClock(time.Time{})
	lim := New(8, clock)
	if got := lim.Stats().Overflows; got != 0 {
		t.Fatalf("Overflows = %d, want 0", got)
	}
	lim.NoteOverflow()
	got := lim.Stats()
	if got.Overflows != 1 || got.LastOverflowAt.IsZero() {
		t.Fatalf("Stats after overflow = %+v", got)
	}
}

func TestLimiter_WaitRespectsCancel(t *testing.T) {
	lim := New(1, realClock{})
	ctx, cancel := context.WithCancel(context.Background())
	if err := lim.Wait(ctx); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	cancel()
	if err := lim.Wait(ctx); err == nil {
		t.Fatal("Wait after cancel: want ctx error")
	}
}

func maxInWindow(stamps []time.Time, window time.Duration) int {
	maxN := 0
	for i, t := range stamps {
		n := 0
		cutoff := t.Add(-window)
		for _, s := range stamps[:i+1] {
			if s.After(cutoff) {
				n++
			}
		}
		if n > maxN {
			maxN = n
		}
	}
	return maxN
}

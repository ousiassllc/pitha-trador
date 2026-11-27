package scheduler

import (
	"context"
	"testing"
	"time"
)

// startedTriggers returns how many cron triggers a Scheduler registers on
// Start with opts.
func startedTriggers(t *testing.T, opts ...Option) int {
	t.Helper()
	s := New(nil, nil, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	return len(s.cron.Entries())
}

// scan.full_scan_enabled: false (issue #652): the cron trigger is not even
// registered, so no full ingestion can happen however many cycles pass. The
// trigger list is inspected instead of waiting out several 1s cycles.
func TestScheduler_Start_DisabledFullScanRegistersNoTrigger(t *testing.T) {
	enabled := startedTriggers(t)
	disabled := startedTriggers(t, WithFullScanDisabled())
	if disabled != enabled-1 {
		t.Errorf("cron triggers with the full scan disabled = %d, want %d (one fewer than the %d with it enabled)", disabled, enabled-1, enabled)
	}
}

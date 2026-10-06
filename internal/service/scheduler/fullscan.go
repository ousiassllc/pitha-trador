package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// WithFullScanDisabled turns off the 60秒 full REST scan (functional.md
// FR-SCHED-2, strategy.yaml scan.full_scan_enabled: false, issue #652):
// Start registers no full-scan cron trigger and EnqueueFullScan enqueues
// nothing, so no market-data ingestion of the whole universe is performed.
// Event-driven jev-scout re-evaluation and the other triggers are unaffected.
func WithFullScanDisabled() Option {
	return func(s *Scheduler) { s.fullScanDisabled = true }
}

// addFullScanTrigger registers the cron trigger that enqueues the full scan
// every interval, or only logs that it is off when WithFullScanDisabled was
// given.
func (s *Scheduler) addFullScanTrigger(ctx context.Context, interval time.Duration) error {
	if s.fullScanDisabled {
		slog.Warn("scheduler: full scan disabled by scan.full_scan_enabled=false: no market-data full ingestion is performed")
		return nil
	}
	spec := fmt.Sprintf("@every %s", interval)
	if _, err := s.cron.AddFunc(spec, func() {
		if _, err := s.EnqueueFullScan(ctx, time.Now().UTC()); err != nil {
			slog.Error("scheduler: full scan enqueue failed", "error", err)
		}
	}); err != nil {
		return fmt.Errorf("scheduler: register full scan trigger %q: %w", spec, err)
	}
	return nil
}

package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// EnqueueMarketData enqueues one market-data job per given instrument, due at
// now (the ranking-driven watch list, functional.md FR-SCHED-9). It is the
// full scan's enqueue minus the 全銘柄 listing, and is not affected by
// WithFullScanDisabled: with the full scan off it is the only market-data
// ingestion. Like EnqueueFullScan it enqueues nothing outside a trading
// session and while the previous cycle's market-data jobs are unfinished.
func (s *Scheduler) EnqueueMarketData(ctx context.Context, instruments []domain.Instrument, now time.Time) (int, error) {
	if len(instruments) == 0 {
		return 0, nil
	}
	return s.enqueueMarketData(ctx, now, "ranking watch", func() ([]domain.Instrument, error) { return instruments, nil })
}

// enqueueMarketData is the enqueue shared by EnqueueFullScan and
// EnqueueMarketData: it enqueues one market-data job per instrument load
// returns, in a single transaction, and returns how many; what names the
// caller in the log lines. See EnqueueFullScan for the session and
// unfinished-cycle gates.
func (s *Scheduler) enqueueMarketData(ctx context.Context, now time.Time, what string, load func() ([]domain.Instrument, error)) (int, error) {
	if !s.inSession(now) {
		return 0, nil
	}
	unfinished, err := s.unfinishedMarketDataJobs(ctx, now)
	if err != nil {
		return 0, err
	}
	if unfinished > 0 {
		slog.Warn("scheduler: "+what+" skipped: previous cycle still running", "pending", unfinished)
		return 0, nil
	}
	instruments, err := load()
	if err != nil {
		return 0, err
	}

	payloads := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		payload, err := json.Marshal(fullScanPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
		if err != nil {
			return 0, fmt.Errorf("scheduler: marshal %s payload for %q: %w", what, inst.Symbol, err)
		}
		payloads = append(payloads, string(payload))
	}
	if _, err := s.jobs.EnqueueBatch(ctx, jobqueue.JobQueueMarketData, payloads, now); err != nil {
		return 0, fmt.Errorf("scheduler: enqueue market-data jobs for %s: %w", what, err)
	}
	slog.Info("scheduler: "+what+" enqueued", "instrument_count", len(instruments))
	return len(instruments), nil
}

// WithFullScanDisabled turns off the 60秒 full REST scan (functional.md
// FR-SCHED-2; the default - strategy.yaml scan.full_scan_enabled is false or
// omitted, issues #651/#652): Start registers no full-scan cron trigger and
// EnqueueFullScan enqueues nothing, so no market-data ingestion of the whole
// universe is performed. The ranking watch list (EnqueueMarketData),
// event-driven jev-scout re-evaluation and the other triggers are unaffected.
func WithFullScanDisabled() Option {
	return func(s *Scheduler) { s.fullScanDisabled = true }
}

// addFullScanTrigger registers the cron trigger that enqueues the full scan
// every interval, or only logs that it is off when WithFullScanDisabled was
// given.
func (s *Scheduler) addFullScanTrigger(ctx context.Context, interval time.Duration) error {
	if s.fullScanDisabled {
		slog.Info("scheduler: full scan off (scan.full_scan_enabled is not true): only the ranking watch list is ingested")
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

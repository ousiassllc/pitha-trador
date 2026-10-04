package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// orphanedRunningAfter is how long a market-data job may stay running
// before the full scan treats it as an orphan: ten times the default
// 60s full-scan interval, far beyond a single instrument's fetch.
const orphanedRunningAfter = 10 * time.Minute

// unfinishedMarketDataJobs returns how many market-data jobs are still
// pending or running, i.e. left over from earlier full-scan cycles. It
// counts only those open rows (jobqueue.JobRepository.CountOpen), never the
// retained finished ones, because it runs every cycle.
//
// Rows that have been running for more than orphanedRunningAfter at now are
// failed first and logged: a worker whose MarkSucceeded/MarkFailed write
// failed leaves its row running, and only Recover (startup) would reset it,
// so counting it would skip every later cycle forever (issue #416).
func (s *Scheduler) unfinishedMarketDataJobs(ctx context.Context, now time.Time) (int, error) {
	orphans, err := s.jobs.FailOrphanedRunning(ctx, jobqueue.JobQueueMarketData,
		now.Add(-orphanedRunningAfter), now,
		fmt.Sprintf("orphaned: still running after %s", orphanedRunningAfter))
	if err != nil {
		return 0, fmt.Errorf("scheduler: recover orphaned market-data jobs: %w", err)
	}
	if len(orphans) > 0 {
		slog.Warn("scheduler: failed orphaned running market-data jobs", "count", len(orphans), "threshold", orphanedRunningAfter.String())
	}

	n, err := s.jobs.CountOpen(ctx, jobqueue.JobQueueMarketData)
	if err != nil {
		return 0, fmt.Errorf("scheduler: count unfinished market-data jobs: %w", err)
	}
	return n, nil
}

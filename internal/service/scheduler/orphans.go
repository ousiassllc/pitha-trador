package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/orphans"
)

// orphanRecoveryCronSpec is how often Start fails the orphaned running
// jobs of every queue (orphans.FailAll).
const orphanRecoveryCronSpec = "@every 1m"

// unfinishedMarketDataJobs returns how many market-data jobs are still
// pending or running, i.e. left over from earlier full-scan cycles. It
// counts only those open rows (jobqueue.JobRepository.CountOpen), never the
// retained finished ones, because it runs every cycle.
//
// Orphans are failed first (orphans.Fail) so that a stuck row cannot make
// every later cycle skip even if the periodic recovery has not run yet.
func (s *Scheduler) unfinishedMarketDataJobs(ctx context.Context, now time.Time) (int, error) {
	if err := orphans.Fail(ctx, s.jobs, jobqueue.JobQueueMarketData, now); err != nil {
		return 0, err
	}
	n, err := s.jobs.CountOpen(ctx, jobqueue.JobQueueMarketData)
	if err != nil {
		return 0, fmt.Errorf("scheduler: count unfinished market-data jobs: %w", err)
	}
	return n, nil
}

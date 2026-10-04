// Package orphans fails jobs left status='running' by a worker whose
// MarkSucceeded/MarkFailed write failed (database is locked, disk error,
// ...). Only scheduler.Recover (startup) would reset such a row, so until
// then it blocks the full scan (issue #416), keeps its instrument held off
// jev-scout forever (candidates.scoutHeld, issues #424/#425) and inflates
// the Activity "Running" count.
package orphans

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// After is how long a job on any queue may stay running before it is
// treated as an orphan. It is a fixed value that does not follow
// scan.full_scan_interval_seconds: ten times the default 60s full-scan
// interval, far beyond the slowest legitimate handler (a Jev call is at
// most ~43s, a market-data fetch is one instrument). A handler that really
// is slower is harmless to fail early, since its own
// MarkSucceeded/MarkFailed later overwrites the status.
const After = 10 * time.Minute

// Jobs is the jobqueue.JobRepository method this package needs.
type Jobs interface {
	FailOrphanedRunning(ctx context.Context, queue string, startedBefore, finishedAt time.Time, reason string) ([]jobqueue.Job, error)
}

// Fail fails the jobs on queue that have been running for more than After
// at now and logs, with the queue name, how many it closed.
func Fail(ctx context.Context, jobs Jobs, queue string, now time.Time) error {
	closed, err := jobs.FailOrphanedRunning(ctx, queue, now.Add(-After), now,
		fmt.Sprintf("orphaned: still running after %s", After))
	if err != nil {
		return fmt.Errorf("orphans: recover orphaned %s jobs: %w", queue, err)
	}
	if len(closed) > 0 {
		slog.Warn("orphans: failed orphaned running jobs", "queue", queue, "count", len(closed), "threshold", After.String())
	}
	return nil
}

// FailAll runs Fail on every queue. A failing queue does not stop the
// others from being recovered; the errors are joined.
func FailAll(ctx context.Context, jobs Jobs, now time.Time) error {
	var errs []error
	for _, queue := range jobqueue.AllQueues() {
		if err := Fail(ctx, jobs, queue, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

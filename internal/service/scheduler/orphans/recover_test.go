package orphans_test

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/orphans"
)

// captureWarnLogs routes slog to a buffer for the test and returns it.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// claimRunning enqueues one job on queue due at startedAt and claims it, so
// its row is running since startedAt.
func claimRunning(t *testing.T, jobs *jobqueue.JobRepository, queue string, startedAt time.Time) jobqueue.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := jobs.Enqueue(ctx, queue, `{"instrument_id":1}`, startedAt); err != nil {
		t.Fatalf("Enqueue(%s): %v", queue, err)
	}
	job, err := jobs.ClaimNext(ctx, queue, startedAt)
	if err != nil {
		t.Fatalf("ClaimNext(%s): %v", queue, err)
	}
	return job
}

// Issues #424/#425: a running row left behind by a failed completion write
// must be failed on every queue - not only market-data - by the recovery, and a row still inside the threshold must be left alone.
func TestFailAll_EveryQueue(t *testing.T) {
	for _, queue := range jobqueue.AllQueues() {
		t.Run(queue, func(t *testing.T) {
			db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
			if err != nil {
				t.Fatalf("sqlitedb.Open: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			jobs := jobqueue.NewJobRepository(db)
			logs := captureWarnLogs(t)
			ctx := context.Background()
			now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

			orphan := claimRunning(t, jobs, queue, now.Add(-11*time.Minute))
			fresh := claimRunning(t, jobs, queue, now.Add(-9*time.Minute))

			if err := orphans.FailAll(ctx, jobs, now); err != nil {
				t.Fatalf("FailAll: %v", err)
			}

			got, err := jobs.Get(ctx, orphan.ID)
			if err != nil {
				t.Fatalf("Get orphan: %v", err)
			}
			if got.Status != jobqueue.JobStatusFailed || got.FinishedAt == nil || got.LastError == nil || !strings.Contains(*got.LastError, "orphaned") {
				t.Errorf("orphan = %+v, want failed with finished_at and an orphaned last_error", got)
			}
			gotFresh, err := jobs.Get(ctx, fresh.ID)
			if err != nil {
				t.Fatalf("Get fresh: %v", err)
			}
			if gotFresh.Status != jobqueue.JobStatusRunning {
				t.Errorf("fresh running job status = %q, want running (within threshold)", gotFresh.Status)
			}
			if out := logs.String(); !strings.Contains(out, "queue="+queue) || !strings.Contains(out, "count=1") {
				t.Errorf("warn log = %q, want queue=%s count=1", out, queue)
			}
		})
	}
}

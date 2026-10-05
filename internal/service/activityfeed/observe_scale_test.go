package activityfeed_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

func seedScaleDB(t *testing.T, finished int) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "scale.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ts := t0.Add(-48 * time.Hour).Format("2006-01-02T15:04:05Z")
	_, err = conn.Exec(`WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		INSERT INTO jobs (queue, payload_json, status, attempts, scheduled_at, started_at, finished_at, created_at)
		SELECT 'market-data', '{}', 'succeeded', 1, ?, ?, ?, ? FROM seq`, finished, ts, ts, ts, ts)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return conn
}

// With tens of thousands of retained finished jobs, a subscriber must not
// make job transitions slower: ObserveJob returns without touching jobs, and
// the coalesced queue update still reports the true depth.
func TestService_ObserveJob_ScaleKeepsWriterPathCheapAndUpdateAccurate(t *testing.T) {
	conn := seedScaleDB(t, 50000)
	repo := jobqueue.NewJobRepository(conn)
	_, decisions, kill := fixtures()
	svc := activityfeed.New(repo, decisions, kill)
	svc.SetQueueUpdateInterval(10 * time.Millisecond)
	repo.SetObserver(svc.ObserveJob)
	messages, cancel := svc.Subscribe()
	defer cancel()

	ctx := context.Background()
	var worst time.Duration
	for range 100 {
		start := time.Now()
		svc.ObserveJob(ctx, jobqueue.Job{Queue: jobqueue.JobQueueMarketData, Status: jobqueue.JobStatusPending, CreatedAt: t0})
		if d := time.Since(start); d > worst {
			worst = d
		}
	}
	if worst > 20*time.Millisecond {
		t.Fatalf("slowest ObserveJob = %v with 50k retained jobs, want it independent of table size (<20ms)", worst)
	}

	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, "{}", time.Now().UTC()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-messages:
			if m.QueueUpdate != nil && m.QueueUpdate.Queue == jobqueue.JobQueueMarketData && m.QueueUpdate.Pending == 1 {
				if m.QueueUpdate.Running != 0 || m.QueueUpdate.FailedRecent != 0 {
					t.Fatalf("update = %+v, want running=0 failedRecent=0", m.QueueUpdate)
				}
				return
			}
		case <-deadline:
			t.Fatal("no market-data queue update with pending=1 after Enqueue")
		}
	}
}

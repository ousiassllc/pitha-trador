package jobqueue

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// seedFinishedJobs bulk-inserts n succeeded jobs on queue (finished long
// ago), the retained-history bulk the hot-path queries must not read.
func seedFinishedJobs(t *testing.T, db *sql.DB, queue string, n int, finishedAt time.Time) {
	t.Helper()
	_, err := db.Exec(`WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		INSERT INTO jobs (queue, payload_json, status, attempts, scheduled_at, started_at, finished_at, created_at)
		SELECT ?, '{}', 'succeeded', 1, ?, ?, ?, ? FROM seq`,
		n, queue, sqlutil.FormatTime(finishedAt), sqlutil.FormatTime(finishedAt), sqlutil.FormatTime(finishedAt), sqlutil.FormatTime(finishedAt))
	if err != nil {
		t.Fatalf("seed finished jobs: %v", err)
	}
}

func explainPlan(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return plan
}

// assertNoFullScan fails when the plan reads jobs without an index range
// search (SQLite reports those as "SCAN jobs ..."; a covering-index
// full scan is "SCAN jobs USING ... INDEX" and is equally rejected).
func assertNoFullScan(t *testing.T, plan []string) {
	t.Helper()
	for _, step := range plan {
		if strings.HasPrefix(step, "SCAN jobs") {
			t.Fatalf("query scans the whole jobs table; plan:\n%s", strings.Join(plan, "\n"))
		}
	}
}

func openPlanDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestQueueCountsQuery_UsesIndexRangeSearches(t *testing.T) {
	db := openPlanDB(t)
	seedFinishedJobs(t, db, JobQueueMarketData, 1000, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	plan := explainPlan(t, db, queueCountsQuery,
		JobQueueMarketData, JobQueueFeatureCalc, JobQueueJevScout, JobQueueJevTrader, JobQueueOutcomeLabeling, JobQueueAnalytics,
		JobStatusPending, JobStatusRunning, JobStatusFailed, "2026-09-01T00:00:00Z")
	assertNoFullScan(t, plan)
}

func TestCountOpenQuery_UsesCoveringIndex(t *testing.T) {
	db := openPlanDB(t)
	seedFinishedJobs(t, db, JobQueueMarketData, 1000, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	plan := explainPlan(t, db, countOpenQuery, JobQueueMarketData, JobStatusPending, JobStatusRunning)
	assertNoFullScan(t, plan)
	// Either (queue, status, ...) index covers the count.
	if !strings.Contains(strings.Join(plan, "\n"), "SEARCH jobs USING COVERING INDEX jobs_queue_status_") {
		t.Fatalf("plan is not a covering-index search:\n%s", strings.Join(plan, "\n"))
	}
}

func TestListOpenOrFinishedSinceQuery_UsesIndexRangeSearches(t *testing.T) {
	db := openPlanDB(t)
	seedFinishedJobs(t, db, JobQueueJevScout, 1000, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	plan := explainPlan(t, db, listOpenOrFinishedSinceQuery,
		JobQueueJevScout, JobStatusPending, JobStatusRunning,
		JobQueueJevScout, JobStatusSucceeded, JobStatusFailed, "2026-09-01T00:00:00Z")
	assertNoFullScan(t, plan)
	if !strings.Contains(strings.Join(plan, "\n"), "jobs_queue_status_finished_idx") {
		t.Fatalf("plan does not use jobs_queue_status_finished_idx:\n%s", strings.Join(plan, "\n"))
	}
}

// With many retained finished rows, the open-job count must stay a tiny
// lookup: this is the per-cycle scheduler path (issue #394).
func TestCountOpen_IgnoresRetainedFinishedRows(t *testing.T) {
	db := openPlanDB(t)
	repo := NewJobRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	seedFinishedJobs(t, db, JobQueueMarketData, 50000, now.Add(-24*time.Hour))
	for range 3 {
		if _, err := repo.Enqueue(ctx, JobQueueMarketData, "{}", now); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	if _, err := repo.Enqueue(ctx, JobQueueFeatureCalc, "{}", now); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := repo.ClaimNext(ctx, JobQueueMarketData, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	start := time.Now()
	n, err := repo.CountOpen(ctx, JobQueueMarketData)
	if err != nil {
		t.Fatalf("CountOpen: %v", err)
	}
	if n != 3 {
		t.Fatalf("CountOpen(market-data) = %d, want 3 (2 pending + 1 running)", n)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("CountOpen took %v over 50k finished rows, want index-bounded (<100ms)", elapsed)
	}
}

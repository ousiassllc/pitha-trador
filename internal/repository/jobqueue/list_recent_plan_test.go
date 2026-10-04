package jobqueue

import (
	"strings"
	"testing"
	"time"
)

// ListRecent backs the Activity snapshot; it must stay bounded by limit and
// the open rows however many finished rows are retained (issue #419).
func TestListRecentQuery_UsesIndexRangeSearchesWithoutSort(t *testing.T) {
	db := openPlanDB(t)
	seedFinishedJobs(t, db, JobQueueMarketData, 1000, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	query, args := listRecentSQL(allQueues, 50)
	plan := explainPlan(t, db, query, args...)
	assertNoFullScan(t, plan)
	joined := strings.Join(plan, "\n")
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("plan sorts rows instead of reading them in index order:\n%s", joined)
	}
	if !strings.Contains(joined, "jobs_queue_status_finished_idx") {
		t.Fatalf("plan does not use jobs_queue_status_finished_idx:\n%s", joined)
	}
}

package activityfeed_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

// A failed job's last_error is an error; the "deferred:"/"skipped:" notes on
// pending/succeeded outcome-labeling jobs are shown as notes so operators do
// not read a pending or skipped job as a failure (issue #710).
func TestService_Snapshot_JobLastErrorIsErrorOnlyWhenFailed(t *testing.T) {
	deferred, skipped, boom := "deferred: not labelable yet", "skipped: lunch break", "boom"
	finished := t0.Add(time.Minute)
	jobs := &fakeJobs{jobs: []jobqueue.Job{
		{ID: 1, Queue: jobqueue.JobQueueOutcomeLabeling, Status: jobqueue.JobStatusPending, Attempts: 1, CreatedAt: t0, LastError: &deferred},
		{ID: 2, Queue: jobqueue.JobQueueOutcomeLabeling, Status: jobqueue.JobStatusSucceeded, Attempts: 1, CreatedAt: t0, FinishedAt: &finished, LastError: &skipped},
		{ID: 3, Queue: jobqueue.JobQueueOutcomeLabeling, Status: jobqueue.JobStatusFailed, Attempts: 1, CreatedAt: t0, LastError: &boom},
	}}
	_, decisions, kill := fixtures()

	snap, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{Type: "job"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var details []string
	for _, e := range snap.Events {
		details = append(details, e.Detail)
	}
	all := strings.Join(details, "\n")
	for _, want := range []string{"status=pending attempts=1 note=deferred: not labelable yet", "status=succeeded attempts=1 note=skipped: lunch break", "status=failed attempts=1 error=boom"} {
		if !strings.Contains(all, want) {
			t.Errorf("details missing %q:\n%s", want, all)
		}
	}
}

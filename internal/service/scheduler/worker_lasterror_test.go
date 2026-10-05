package scheduler_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func TestScheduler_Start_HugeHandlerErrorIsTruncatedInLastError(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{}`, time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	runOnePoll(t, jobs, instruments, func(context.Context, jobqueue.Job) error {
		return errors.New("policy: failed: " + strings.Repeat("e", 1<<20))
	})

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusFailed || got.LastError == nil {
		t.Fatalf("job = %+v, want failed with LastError", got)
	}
	if n := len(*got.LastError); n > 1024 || !strings.HasPrefix(*got.LastError, "policy: failed: ") {
		t.Fatalf("len(LastError) = %d (prefix %.20q), want <= 1024 keeping the leading context", n, *got.LastError)
	}
}

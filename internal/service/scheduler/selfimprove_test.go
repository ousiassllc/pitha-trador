package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func TestScheduler_EnqueueSelfImprove_EnqueuesOneAnalyticsJob(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	now := time.Date(2026, 9, 27, 6, 30, 0, 0, time.UTC)

	if err := s.EnqueueSelfImprove(context.Background(), now); err != nil {
		t.Fatalf("EnqueueSelfImprove: %v", err)
	}

	job, err := jobs.ClaimNext(context.Background(), repository.JobQueueAnalytics, now)
	if err != nil {
		t.Fatalf("ClaimNext(%q): %v", repository.JobQueueAnalytics, err)
	}
	if job.PayloadJSON != "{}" {
		t.Fatalf("job.PayloadJSON = %q, want {}", job.PayloadJSON)
	}

	if _, err := jobs.ClaimNext(context.Background(), repository.JobQueueAnalytics, now); !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("ClaimNext second call error = %v, want ErrJobNotFound (only one job enqueued)", err)
	}
}

func TestScheduler_Start_RegistersDailySelfImproveTrigger(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start must register the daily self-improve cron entry without error.
	if err := s.Start(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
}

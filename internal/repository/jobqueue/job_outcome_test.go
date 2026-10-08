package jobqueue_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func TestJobRepository_Reschedule_PutsRunningJobBackToPendingWithNote(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueOutcomeLabeling, "{}", base); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := repo.ClaimNext(ctx, jobqueue.JobQueueOutcomeLabeling, base)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	var notified []string
	repo.SetObserver(func(_ context.Context, job jobqueue.Job) { notified = append(notified, job.Status) })

	retryAt := base.Add(time.Minute)
	if err := repo.Reschedule(ctx, claimed.ID, retryAt, "data not landed"); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
	got, err := repo.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusPending || !got.ScheduledAt.Equal(retryAt) || got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatalf("job = %+v, want pending, scheduled at %v, no start/finish", got, retryAt)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (kept)", got.Attempts)
	}
	if got.LastError == nil || !strings.HasPrefix(*got.LastError, "deferred: ") || !strings.Contains(*got.LastError, "data not landed") {
		t.Errorf("LastError = %v, want a deferred: note", got.LastError)
	}
	if len(notified) != 1 || notified[0] != jobqueue.JobStatusPending {
		t.Errorf("observer statuses = %v, want [pending]", notified)
	}

	// Not due before retryAt, claimable again at retryAt.
	if _, err := repo.ClaimNext(ctx, jobqueue.JobQueueOutcomeLabeling, base); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Fatalf("ClaimNext before retryAt error = %v, want ErrJobNotFound", err)
	}
	if _, err := repo.ClaimNext(ctx, jobqueue.JobQueueOutcomeLabeling, retryAt); err != nil {
		t.Fatalf("ClaimNext at retryAt: %v", err)
	}
}

func TestJobRepository_Reschedule_NonRunningJobIsNotFound(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	job, err := repo.Enqueue(ctx, jobqueue.JobQueueOutcomeLabeling, "{}", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := repo.Reschedule(ctx, job.ID, time.Now().UTC(), "x"); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Fatalf("Reschedule(pending job) error = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepository_MarkSkipped_EndsSucceededWithSkippedNote(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	job, err := repo.Enqueue(ctx, jobqueue.JobQueueOutcomeLabeling, "{}", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := repo.MarkSkipped(ctx, job.ID, time.Now().UTC(), "lunch break"); err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (a skip is not a failure)", got.Status)
	}
	if got.LastError == nil || *got.LastError != "skipped: lunch break" {
		t.Fatalf("LastError = %v, want %q", got.LastError, "skipped: lunch break")
	}
}

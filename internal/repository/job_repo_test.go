package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestJobRepository_EnqueueAndGet(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	scheduledAt := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

	job, err := repo.Enqueue(ctx, repository.JobQueueMarketData, `{"symbol":"7203"}`, scheduledAt)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if job.Status != repository.JobStatusPending {
		t.Fatalf("Enqueue() Status = %q, want %q", job.Status, repository.JobStatusPending)
	}
	if job.Attempts != 0 {
		t.Fatalf("Enqueue() Attempts = %d, want 0", job.Attempts)
	}

	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", job.ID, err)
	}
	if got.Queue != repository.JobQueueMarketData || got.PayloadJSON != `{"symbol":"7203"}` {
		t.Fatalf("Get(%d) = %+v, want Queue/PayloadJSON to match Enqueue input", job.ID, got)
	}
	if got.StartedAt != nil || got.FinishedAt != nil || got.LastError != nil {
		t.Fatalf("Get(%d) = %+v, want StartedAt/FinishedAt/LastError all nil for a pending job", job.ID, got)
	}
}

func TestJobRepository_ClaimNext_OnlyReturnsDuePendingJobsOnQueue(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

	due, err := repo.Enqueue(ctx, repository.JobQueueFeatureCalc, "{}", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Enqueue due job: %v", err)
	}
	if _, err := repo.Enqueue(ctx, repository.JobQueueFeatureCalc, "{}", now.Add(time.Hour)); err != nil {
		t.Fatalf("Enqueue future job: %v", err)
	}
	if _, err := repo.Enqueue(ctx, repository.JobQueueJevScout, "{}", now.Add(-time.Minute)); err != nil {
		t.Fatalf("Enqueue other-queue job: %v", err)
	}

	claimed, err := repo.ClaimNext(ctx, repository.JobQueueFeatureCalc, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.ID != due.ID {
		t.Fatalf("ClaimNext() claimed job %d, want the due job %d", claimed.ID, due.ID)
	}
	if claimed.Status != repository.JobStatusRunning {
		t.Fatalf("ClaimNext() Status = %q, want %q", claimed.Status, repository.JobStatusRunning)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("ClaimNext() Attempts = %d, want 1", claimed.Attempts)
	}
	if claimed.StartedAt == nil {
		t.Fatalf("ClaimNext() StartedAt = nil, want populated")
	}

	// Claiming again must not return the already-running job nor the
	// not-yet-due future job.
	if _, err := repo.ClaimNext(ctx, repository.JobQueueFeatureCalc, now); !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("second ClaimNext() error = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepository_ClaimNext_NoneDue(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))

	_, err := repo.ClaimNext(context.Background(), repository.JobQueueAnalytics, time.Now().UTC())
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("ClaimNext() on empty queue error = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepository_MarkSucceeded(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()

	job, err := repo.Enqueue(ctx, repository.JobQueueRiskCheck, "{}", now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := repo.ClaimNext(ctx, repository.JobQueueRiskCheck, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	finishedAt := now.Add(time.Second)
	if err := repo.MarkSucceeded(ctx, job.ID, finishedAt); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", job.ID, err)
	}
	if got.Status != repository.JobStatusSucceeded {
		t.Fatalf("Get(%d).Status = %q, want %q", job.ID, got.Status, repository.JobStatusSucceeded)
	}
	if got.FinishedAt == nil {
		t.Fatalf("Get(%d).FinishedAt = nil, want populated", job.ID)
	}
}

func TestJobRepository_MarkFailed_RecordsLastError(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()

	job, err := repo.Enqueue(ctx, repository.JobQueuePaperExecution, "{}", now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := repo.MarkFailed(ctx, job.ID, now, "kabu station timeout"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", job.ID, err)
	}
	if got.Status != repository.JobStatusFailed {
		t.Fatalf("Get(%d).Status = %q, want %q", job.ID, got.Status, repository.JobStatusFailed)
	}
	if got.LastError == nil || *got.LastError != "kabu station timeout" {
		t.Fatalf("Get(%d).LastError = %v, want \"kabu station timeout\"", job.ID, got.LastError)
	}
}

func TestJobRepository_MarkSucceeded_NotFound(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))

	err := repo.MarkSucceeded(context.Background(), 999999, time.Now().UTC())
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("MarkSucceeded(unknown id) error = %v, want ErrJobNotFound", err)
	}
}

func TestJobRepository_ResetStuckRunning(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()

	running, err := repo.Enqueue(ctx, repository.JobQueueOutcomeLabeling, "{}", now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := repo.ClaimNext(ctx, repository.JobQueueOutcomeLabeling, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if _, err := repo.Enqueue(ctx, repository.JobQueueAnalytics, "{}", now); err != nil {
		t.Fatalf("Enqueue pending job: %v", err)
	}

	reset, err := repo.ResetStuckRunning(ctx)
	if err != nil {
		t.Fatalf("ResetStuckRunning: %v", err)
	}
	if reset != 1 {
		t.Fatalf("ResetStuckRunning() = %d, want 1 (only the running job)", reset)
	}

	got, err := repo.Get(ctx, running.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", running.ID, err)
	}
	if got.Status != repository.JobStatusPending {
		t.Fatalf("Get(%d).Status = %q after ResetStuckRunning, want %q", running.ID, got.Status, repository.JobStatusPending)
	}
	if got.StartedAt != nil {
		t.Fatalf("Get(%d).StartedAt = %v after ResetStuckRunning, want nil", running.ID, got.StartedAt)
	}
}

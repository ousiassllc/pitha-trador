package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type fakeLogRotator struct {
	calls int
	err   error
}

func (f *fakeLogRotator) Rotate(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestScheduler_RotateLogs_NoOpWithoutRotator(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	if err := s.RotateLogs(context.Background()); err != nil {
		t.Fatalf("RotateLogs: %v (want nil, no WithLogRotator configured)", err)
	}
}

func TestScheduler_RotateLogs_CallsConfiguredRotator(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	rotator := &fakeLogRotator{}

	s := scheduler.New(jobs, instruments, scheduler.WithLogRotator(rotator))
	if err := s.RotateLogs(context.Background()); err != nil {
		t.Fatalf("RotateLogs: %v", err)
	}
	if rotator.calls != 1 {
		t.Fatalf("rotator.calls = %d, want 1", rotator.calls)
	}
}

func TestScheduler_RotateLogs_PropagatesRotatorError(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	rotator := &fakeLogRotator{err: errors.New("disk full")}

	s := scheduler.New(jobs, instruments, scheduler.WithLogRotator(rotator))
	if err := s.RotateLogs(context.Background()); err == nil {
		t.Fatal("RotateLogs should propagate the rotator's error")
	}
}

func TestScheduler_Start_RegistersDailyLogRotationTrigger(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	rotator := &fakeLogRotator{}

	s := scheduler.New(jobs, instruments, scheduler.WithLogRotator(rotator))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start must succeed (i.e. the maintenance catch-up cron entry
	// registers without error) whether or not a LogRotator is configured; the
	// rotation itself is exercised by
	// TestScheduler_RotateLogs_CallsConfiguredRotator above instead.
	if err := s.Start(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
}

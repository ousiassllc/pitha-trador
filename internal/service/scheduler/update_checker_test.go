package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type fakeUpdateChecker struct {
	calls int
	err   error
}

func (f *fakeUpdateChecker) CheckForUpdate(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestScheduler_CheckForUpdate_NoOpWithoutChecker(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	if err := s.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v (want nil, no WithUpdateChecker configured)", err)
	}
}

func TestScheduler_CheckForUpdate_CallsConfiguredChecker(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	checker := &fakeUpdateChecker{}

	s := scheduler.New(jobs, instruments, scheduler.WithUpdateChecker(checker))
	if err := s.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("checker.calls = %d, want 1", checker.calls)
	}
}

func TestScheduler_CheckForUpdate_PropagatesCheckerError(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	wantErr := errors.New("github api unavailable")
	checker := &fakeUpdateChecker{err: wantErr}

	s := scheduler.New(jobs, instruments, scheduler.WithUpdateChecker(checker))
	if err := s.CheckForUpdate(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("CheckForUpdate: %v, want %v", err, wantErr)
	}
}

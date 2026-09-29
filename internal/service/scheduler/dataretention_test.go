package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type fakePurger struct {
	calls int
	err   error
}

func (f *fakePurger) Purge(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestScheduler_PurgeExpiredData_NoOpWithoutPurger(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db))
	if err := s.PurgeExpiredData(context.Background()); err != nil {
		t.Fatalf("PurgeExpiredData: %v (want nil, no WithDataPurger configured)", err)
	}
}

func TestScheduler_PurgeExpiredData_CallsPurgerAndPropagatesError(t *testing.T) {
	db := newTestDB(t)
	jobs := repository.NewJobRepository(db)
	instruments := repository.NewInstrumentRepository(db)

	ok := &fakePurger{}
	s := scheduler.New(jobs, instruments, scheduler.WithDataPurger(ok))
	if err := s.PurgeExpiredData(context.Background()); err != nil {
		t.Fatalf("PurgeExpiredData: %v", err)
	}
	if ok.calls != 1 {
		t.Fatalf("purger.calls = %d, want 1", ok.calls)
	}

	failing := &fakePurger{err: errors.New("db locked")}
	s = scheduler.New(jobs, instruments, scheduler.WithDataPurger(failing))
	if err := s.PurgeExpiredData(context.Background()); err == nil {
		t.Fatal("PurgeExpiredData should propagate the purger's error")
	}
}

func TestScheduler_Start_RegistersDailyDataPurgeTrigger(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db),
		scheduler.WithDataPurger(&fakePurger{}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
}

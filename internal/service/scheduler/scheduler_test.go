package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateInstrument(t *testing.T, repo *repository.InstrumentRepository, symbol string, active bool) domain.Instrument {
	t.Helper()
	inst, err := repo.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: active,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func TestScheduler_EnqueueFullScan_OnlyActiveInstruments(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	mustCreateInstrument(t, instruments, "7203", true)
	mustCreateInstrument(t, instruments, "9433", true)
	mustCreateInstrument(t, instruments, "1301", false)

	s := scheduler.New(jobs, instruments)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	count, err := s.EnqueueFullScan(context.Background(), now)
	if err != nil {
		t.Fatalf("EnqueueFullScan: %v", err)
	}
	if count != 2 {
		t.Fatalf("EnqueueFullScan count = %d, want 2 (inactive instrument excluded)", count)
	}

	for _, queue := range []string{repository.JobQueueMarketData, repository.JobQueueFeatureCalc} {
		claimed := 0
		for {
			if _, err := jobs.ClaimNext(context.Background(), queue, now); err != nil {
				if errors.Is(err, repository.ErrJobNotFound) {
					break
				}
				t.Fatalf("ClaimNext(%q): %v", queue, err)
			}
			claimed++
		}
		if claimed != 2 {
			t.Errorf("queue %q had %d due jobs, want 2 (one per active instrument)", queue, claimed)
		}
	}
}

func TestScheduler_Recover_ResetsStuckRunningJobs(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	job, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{}`, now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := jobs.ClaimNext(context.Background(), repository.JobQueueMarketData, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	s := scheduler.New(jobs, instruments)
	n, err := s.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 1 {
		t.Fatalf("Recover reset %d jobs, want 1", n)
	}

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != repository.JobStatusPending {
		t.Errorf("job status after Recover = %q, want %q", got.Status, repository.JobStatusPending)
	}
}

func TestScheduler_Start_RunsRegisteredHandlerAndMarksSucceeded(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	now := time.Now().UTC()
	job, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{"symbol":"7203"}`, now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	processed := make(chan repository.Job, 1)
	s := scheduler.New(jobs, instruments, scheduler.WithPollInterval(5*time.Millisecond))
	s.RegisterHandler(repository.JobQueueMarketData, func(_ context.Context, j repository.Job) error {
		processed <- j
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	select {
	case got := <-processed:
		if got.ID != job.ID {
			t.Errorf("processed job ID = %d, want %d", got.ID, job.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the registered handler to run")
	}

	// Give MarkSucceeded (called right after the handler returns) a moment
	// to land before asserting on it.
	deadline := time.Now().Add(1 * time.Second)
	for {
		got, err := jobs.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status == repository.JobStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job status = %q, want %q", got.Status, repository.JobStatusSucceeded)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestScheduler_Start_HandlerErrorMarksJobFailed(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	now := time.Now().UTC()
	job, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{}`, now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	s := scheduler.New(jobs, instruments, scheduler.WithPollInterval(5*time.Millisecond))
	s.RegisterHandler(repository.JobQueueMarketData, func(context.Context, repository.Job) error {
		return errors.New("kabu station api unreachable")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := jobs.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status == repository.JobStatusFailed {
			if got.LastError == nil || *got.LastError != "kabu station api unreachable" {
				t.Errorf("LastError = %v, want the handler's error message", got.LastError)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job status = %q, want %q", got.Status, repository.JobStatusFailed)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestScheduler_Start_FullScanTriggerEnqueuesOnSchedule(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	mustCreateInstrument(t, instruments, "7203", true)

	s := scheduler.New(jobs, instruments)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, 1*time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := jobs.ClaimNext(context.Background(), repository.JobQueueMarketData, time.Now().UTC()); err == nil {
			return
		} else if !errors.Is(err, repository.ErrJobNotFound) {
			t.Fatalf("ClaimNext: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the full scan cron trigger to enqueue a market-data job")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

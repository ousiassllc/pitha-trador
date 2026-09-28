package scheduler_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type fakeHeartbeatChecker struct {
	calls int
	err   error
}

func (f *fakeHeartbeatChecker) CheckHeartbeatTimeout(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestScheduler_CheckOperatorHeartbeat_NoOpWithoutChecker(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	if err := s.CheckOperatorHeartbeat(context.Background()); err != nil {
		t.Fatalf("CheckOperatorHeartbeat: %v (want nil, no WithHeartbeatChecker configured)", err)
	}
}

func TestScheduler_CheckOperatorHeartbeat_CallsConfiguredChecker(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	checker := &fakeHeartbeatChecker{}

	s := scheduler.New(jobs, instruments, scheduler.WithHeartbeatChecker(checker))
	if err := s.CheckOperatorHeartbeat(context.Background()); err != nil {
		t.Fatalf("CheckOperatorHeartbeat: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("checker.calls = %d, want 1", checker.calls)
	}
}

func TestScheduler_CheckOperatorHeartbeat_PropagatesCheckerError(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	wantErr := errors.New("kill switch already active")
	checker := &fakeHeartbeatChecker{err: wantErr}

	s := scheduler.New(jobs, instruments, scheduler.WithHeartbeatChecker(checker))
	if err := s.CheckOperatorHeartbeat(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("CheckOperatorHeartbeat error = %v, want %v", err, wantErr)
	}
}

func TestScheduler_EnqueueEventReevaluation_NoOpWhenNotTriggered(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203", true)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	s := scheduler.New(jobs, instruments)
	if err := s.EnqueueEventReevaluation(context.Background(), inst.ID, inst.Symbol, false, now); err != nil {
		t.Fatalf("EnqueueEventReevaluation: %v", err)
	}

	if _, err := jobs.ClaimNext(context.Background(), repository.JobQueueJevScout, now); !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("ClaimNext(jev-scout) error = %v, want ErrJobNotFound (FR-SCAN-2 suppression: no job enqueued)", err)
	}
}

func TestScheduler_EnqueueEventReevaluation_EnqueuesImmediatelyWhenTriggered(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203", true)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	s := scheduler.New(jobs, instruments)
	if err := s.EnqueueEventReevaluation(context.Background(), inst.ID, inst.Symbol, true, now); err != nil {
		t.Fatalf("EnqueueEventReevaluation: %v", err)
	}

	job, err := jobs.ClaimNext(context.Background(), repository.JobQueueJevScout, now)
	if err != nil {
		t.Fatalf("ClaimNext(jev-scout): %v", err)
	}
	var payload struct {
		InstrumentID int64  `json:"instrument_id"`
		Symbol       string `json:"symbol"`
	}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.InstrumentID != inst.ID || payload.Symbol != inst.Symbol {
		t.Fatalf("payload = %+v, want instrument_id=%d symbol=%q", payload, inst.ID, inst.Symbol)
	}
}

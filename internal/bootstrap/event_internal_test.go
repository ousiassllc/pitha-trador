package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func eventTestBars(inst domain.Instrument, prevVWAPBps, currVWAPBps float64) (domain.Snapshot, []domain.Snapshot) {
	at := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at.Add(-time.Minute), Price: 2500,
		Feature: domain.Feature{PriceVsVWAPBps: prevVWAPBps}}
	curr := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, Price: 2500,
		Feature: domain.Feature{PriceVsVWAPBps: currVWAPBps}}
	return curr, []domain.Snapshot{prev}
}

func claimJevScout(t *testing.T, svc *Services) (repository.Job, bool) {
	t.Helper()
	job, err := svc.Jobs.ClaimNext(context.Background(), repository.JobQueueJevScout, time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC))
	if errors.Is(err, repository.ErrJobNotFound) {
		return repository.Job{}, false
	}
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	return job, true
}

func TestEnqueueEventReevaluation_CandidateWithVWAPCrossEnqueuesJevScout(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")
	svc.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())

	curr, history := eventTestBars(inst, -5, 5)
	if err := svc.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if _, ok := claimJevScout(t, svc); !ok {
		t.Fatal("no jev-scout job enqueued, want an immediate re-evaluation for a VWAP cross (FR-SCAN-1)")
	}
}

func TestEnqueueEventReevaluation_QuietCandidateIsSuppressed(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")
	svc.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())

	curr, history := eventTestBars(inst, 5, 5)
	if err := svc.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, svc); ok {
		t.Fatalf("jev-scout job %+v enqueued for a quiet bar, want FR-SCAN-2 suppression", job)
	}
}

func TestEnqueueEventReevaluation_NonCandidateIsSkipped(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")

	curr, history := eventTestBars(inst, -5, 5)
	if err := svc.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, svc); ok {
		t.Fatalf("jev-scout job %+v enqueued for a non-candidate, want none", job)
	}
}

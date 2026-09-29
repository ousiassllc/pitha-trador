package scheduler_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func mustInsertTraderDecision(t *testing.T, decisions *repository.DecisionRepository, instID int64, symbol string, timestamp time.Time) domain.JevDecision {
	t.Helper()
	direction := domain.JevDirectionLong
	confidence := 0.8
	saved, err := decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instID, Symbol: symbol, Timestamp: timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
		Direction: &direction, Confidence: &confidence, ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert trader decision: %v", err)
	}
	return saved
}

func TestScheduler_EnqueueOutcomeLabeling_NoOpWithoutSource(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	s := scheduler.New(jobs, instruments)
	count, err := s.EnqueueOutcomeLabeling(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("EnqueueOutcomeLabeling: %v", err)
	}
	if count != 0 {
		t.Fatalf("EnqueueOutcomeLabeling count = %d, want 0 (no WithOutcomeLabelSource configured)", count)
	}
}

func TestScheduler_EnqueueOutcomeLabeling_EnqueuesDueDecisions(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	decisions := repository.NewDecisionRepository(db)
	outcomes := repository.NewCalibrationRepository(db)

	inst := mustCreateInstrument(t, instruments, "7203", true)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	now := base.Add(21 * time.Minute)
	due := mustInsertTraderDecision(t, decisions, inst.ID, "7203", base)
	notDueYet := mustInsertTraderDecision(t, decisions, inst.ID, "7203", now.Add(-1*time.Minute))

	s := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes))
	count, err := s.EnqueueOutcomeLabeling(context.Background(), now)
	if err != nil {
		t.Fatalf("EnqueueOutcomeLabeling: %v", err)
	}
	// due's 5/10/20-minute horizons have all elapsed by now (21 minutes
	// later); notDueYet's have not (decided 1 minute ago).
	if count != len(scheduler.DefaultOutcomeLabelHorizonsMinutes) {
		t.Fatalf("EnqueueOutcomeLabeling count = %d, want %d (one per horizon for the one due decision)",
			count, len(scheduler.DefaultOutcomeLabelHorizonsMinutes))
	}

	seenHorizons := make(map[int]bool)
	for {
		job, err := jobs.ClaimNext(context.Background(), repository.JobQueueOutcomeLabeling, now)
		if err != nil {
			if errors.Is(err, repository.ErrJobNotFound) {
				break
			}
			t.Fatalf("ClaimNext(%q): %v", repository.JobQueueOutcomeLabeling, err)
		}
		var payload repository.OutcomeLabelJobPayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.JevDecisionID != due.ID {
			t.Fatalf("enqueued job for decision %d, want only due decision %d (not %d)", payload.JevDecisionID, due.ID, notDueYet.ID)
		}
		seenHorizons[payload.HorizonMinutes] = true
	}
	for _, h := range scheduler.DefaultOutcomeLabelHorizonsMinutes {
		if !seenHorizons[h] {
			t.Errorf("missing enqueued outcome-labeling job for horizon %dm", h)
		}
	}
}

package outcomeflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// Issue #760: the triple-barrier timeout is a Settings value, so the scan
// labels each decision at the horizons WithOutcomeLabelHorizons resolves
// (read on every scan) instead of the 5/10/15 defaults.
func TestScheduler_EnqueueOutcomeLabeling_UsesConfiguredHorizons(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203", true)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	now := base.Add(21 * time.Minute)
	due := mustInsertTraderDecision(t, decisions, inst.ID, "7203", base)

	timeout := 7
	s := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes),
		scheduler.WithOutcomeLabelHorizons(func(context.Context) ([]int, error) { return []int{timeout}, nil }))
	count, err := s.EnqueueOutcomeLabeling(context.Background(), now)
	if err != nil || count != 1 {
		t.Fatalf("EnqueueOutcomeLabeling = %d, %v, want 1 job for the 7-minute timeout", count, err)
	}
	job, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueOutcomeLabeling, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	var payload calrepo.OutcomeLabelJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.JevDecisionID != due.ID || payload.HorizonMinutes != 7 {
		t.Fatalf("payload = %+v, want decision %d at horizon 7", payload, due.ID)
	}

	// A resolver failure is surfaced, not swallowed into the defaults.
	boom := errors.New("boom")
	failing := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes),
		scheduler.WithOutcomeLabelHorizons(func(context.Context) ([]int, error) { return nil, boom }))
	if _, err := failing.EnqueueOutcomeLabeling(context.Background(), now); !errors.Is(err, boom) {
		t.Fatalf("EnqueueOutcomeLabeling error = %v, want %v", err, boom)
	}
}

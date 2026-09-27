package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func TestScheduler_EnqueueOutcomeLabeling_StopsRetryingDecisionsPastRetryWindow(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
	decisions := repository.NewDecisionRepository(db)
	outcomes := repository.NewCalibrationRepository(db)

	inst := mustCreateInstrument(t, instruments, "7203", true)
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	// Never labeled (e.g. its horizon window had no market data), and
	// decided more than a day ago: it must no longer be re-enqueued.
	mustInsertTraderDecision(t, decisions, inst.ID, "7203", now.Add(-25*time.Hour))
	recent := mustInsertTraderDecision(t, decisions, inst.ID, "7203", now.Add(-30*time.Minute))

	s := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes))
	count, err := s.EnqueueOutcomeLabeling(context.Background(), now)
	if err != nil {
		t.Fatalf("EnqueueOutcomeLabeling: %v", err)
	}
	if want := len(scheduler.DefaultOutcomeLabelHorizonsMinutes); count != want {
		t.Fatalf("EnqueueOutcomeLabeling count = %d, want %d (only decision %d's horizons)", count, want, recent.ID)
	}
}

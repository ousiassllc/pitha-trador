// Package outcomeflow_test holds the Scheduler.EnqueueOutcomeLabeling tests
// (pending-pair scan, dedupe against open jobs, skipped-pair exclusion).
// They only use scheduler's exported API and live in their own directory to
// keep internal/service/scheduler under the linterly line budget (#481).
// The helpers below are this package's own (sibling test packages do not
// import each other).
package outcomeflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateInstrument(t *testing.T, repo *market.InstrumentRepository, symbol string, active bool) domain.Instrument {
	t.Helper()
	inst, err := repo.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: active,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func mustInsertTraderDecision(t *testing.T, decisions *judgement.DecisionRepository, instID int64, symbol string, timestamp time.Time) domain.JevDecision {
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
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

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
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)

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
	// due's 5/10/15-minute horizons have all elapsed by now (21 minutes
	// later); notDueYet's have not (decided 1 minute ago).
	if count != len(scheduler.DefaultOutcomeLabelHorizonsMinutes) {
		t.Fatalf("EnqueueOutcomeLabeling count = %d, want %d (one per horizon for the one due decision)",
			count, len(scheduler.DefaultOutcomeLabelHorizonsMinutes))
	}

	seenHorizons := make(map[int]bool)
	for {
		job, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueOutcomeLabeling, now)
		if err != nil {
			if errors.Is(err, jobqueue.ErrJobNotFound) {
				break
			}
			t.Fatalf("ClaimNext(%q): %v", jobqueue.JobQueueOutcomeLabeling, err)
		}
		var payload calrepo.OutcomeLabelJobPayload
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

func TestScheduler_EnqueueOutcomeLabeling_StopsRetryingDecisionsPastRetryWindow(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)

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

func countLabelingJobs(t *testing.T, jobs *jobqueue.JobRepository, status string) int {
	t.Helper()
	all, err := jobs.ListRecent(context.Background(), jobqueue.JobQueueOutcomeLabeling, 10000)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	n := 0
	for _, j := range all {
		if j.Status == status {
			n++
		}
	}
	return n
}

// Re-scanning every minute must not stack a second job for a pair that
// already has a pending or running one (issue #481).
func TestScheduler_EnqueueOutcomeLabeling_DedupesPendingAndRunningPairs(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instruments, "7203", true)
	base := time.Date(2026, 9, 27, 15, 25, 0, 0, time.UTC)
	mustInsertTraderDecision(t, decisions, inst.ID, "7203", base)
	horizons := len(scheduler.DefaultOutcomeLabelHorizonsMinutes)

	s := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes))
	now := base.Add(21 * time.Minute)
	for i := range 5 {
		count, err := s.EnqueueOutcomeLabeling(ctx, now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("EnqueueOutcomeLabeling #%d: %v", i, err)
		}
		want := 0
		if i == 0 {
			want = horizons
		}
		if count != want {
			t.Fatalf("EnqueueOutcomeLabeling #%d count = %d, want %d (pending pairs already queued)", i, count, want)
		}
	}

	// A claimed (running) job still blocks a duplicate.
	running, err := jobs.ClaimNext(ctx, jobqueue.JobQueueOutcomeLabeling, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if count, err := s.EnqueueOutcomeLabeling(ctx, now.Add(10*time.Minute)); err != nil || count != 0 {
		t.Fatalf("EnqueueOutcomeLabeling with a running job = (%d, %v), want (0, nil)", count, err)
	}

	// Once that job fails (data not landed yet) the pair is retried -
	// exactly once more, not once per scan.
	if err := jobs.MarkFailed(ctx, running.ID, now, "data not landed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	for i, want := range []int{1, 0, 0} {
		count, err := s.EnqueueOutcomeLabeling(ctx, now.Add(time.Duration(11+i)*time.Minute))
		if err != nil {
			t.Fatalf("EnqueueOutcomeLabeling retry #%d: %v", i, err)
		}
		if count != want {
			t.Fatalf("EnqueueOutcomeLabeling retry #%d count = %d, want %d", i, count, want)
		}
	}
	if n := countLabelingJobs(t, jobs, jobqueue.JobStatusFailed); n != 1 {
		t.Fatalf("failed jobs = %d, want 1", n)
	}
}

// A pair the labeler marked permanently unlabelable is never re-enqueued
// again, however many scans run (issue #481 acceptance: no unbounded job
// growth for a 15:25 decision's 15-minute horizon).
func TestScheduler_EnqueueOutcomeLabeling_SkipsPermanentlyUnlabelablePairs(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instruments, "7203", true)
	base := time.Date(2026, 9, 27, 15, 25, 0, 0, time.UTC)
	decision := mustInsertTraderDecision(t, decisions, inst.ID, "7203", base)
	if err := outcomes.MarkUnlabelable(ctx, decision.ID, 15, "window closed at 15:30"); err != nil {
		t.Fatalf("MarkUnlabelable: %v", err)
	}

	s := scheduler.New(jobs, instruments, scheduler.WithOutcomeLabelSource(outcomes))
	for i := range 30 {
		if _, err := s.EnqueueOutcomeLabeling(ctx, base.Add(time.Duration(21+i)*time.Minute)); err != nil {
			t.Fatalf("EnqueueOutcomeLabeling: %v", err)
		}
		// Drain: every enqueued job fails at once, as an unlabelable pair's
		// did before the skip marker existed.
		for {
			job, err := jobs.ClaimNext(ctx, jobqueue.JobQueueOutcomeLabeling, base.Add(time.Hour))
			if err != nil {
				break
			}
			if err := jobs.MarkFailed(ctx, job.ID, base.Add(time.Hour), "x"); err != nil {
				t.Fatalf("MarkFailed: %v", err)
			}
		}
	}
	if n := countLabelingJobsForHorizon(t, jobs, 15); n != 0 {
		t.Fatalf("outcome-labeling jobs for the skipped 15m pair = %d, want 0", n)
	}
}

func countLabelingJobsForHorizon(t *testing.T, jobs *jobqueue.JobRepository, horizon int) int {
	t.Helper()
	all, err := jobs.ListRecent(context.Background(), jobqueue.JobQueueOutcomeLabeling, 10000)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	n := 0
	for _, j := range all {
		var p calrepo.OutcomeLabelJobPayload
		if err := json.Unmarshal([]byte(j.PayloadJSON), &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.HorizonMinutes == horizon {
			n++
		}
	}
	return n
}

// Issue #711: Outcome Labeling's judgment horizons are the short-hold
// 5/10/15 minutes, not the former 5/10/20.
func TestDefaultOutcomeLabelHorizonsMinutes_AreFiveTenFifteen(t *testing.T) {
	if want := []int{5, 10, 15}; !reflect.DeepEqual(scheduler.DefaultOutcomeLabelHorizonsMinutes, want) {
		t.Fatalf("DefaultOutcomeLabelHorizonsMinutes = %v, want %v", scheduler.DefaultOutcomeLabelHorizonsMinutes, want)
	}
}

package candidates

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// newScoutCooldownRefresher builds a Refresher with one always-passing
// candidate (7203), a 60s jev-scout cooldown and a fake clock the caller
// advances through the returned pointer.
func newScoutCooldownRefresher(t *testing.T) (*Refresher, domain.Instrument, *time.Time) {
	t.Helper()
	refresher := newTestRefresher(t)
	clock := time.Now().UTC()
	refresher.Now = func() time.Time { return clock }
	refresher.InSession = func(time.Time) bool { return true }
	refresher.Strategy.Scan.JevScoutMinIntervalSeconds = 60
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000, MaxSpreadBps: 100, TopN: 10,
	}
	inst := mustCreateInstrument(t, refresher, "7203")
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: clock,
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{VWAP: 2490, PriceVsVWAPBps: 40, VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01)},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	return refresher, inst, &clock
}

// Issue #388: running Refresh on the 15-30s cadence must not run Jev Scout
// for the same symbol more than once a minute (non-functional.md §2.1).
func TestRefresh_ScoutsEachCandidateAtMostOncePerMinute(t *testing.T) {
	refresher, _, clock := newScoutCooldownRefresher(t)
	ctx := context.Background()

	var scoutedAt []time.Time
	start := *clock
	for _, step := range []time.Duration{0, 20, 40, 60, 80, 100, 120, 140, 160, 180} {
		*clock = start.Add(step * time.Second)
		if err := refresher.Refresh(ctx); err != nil {
			t.Fatalf("Refresh at +%ds: %v", step, err)
		}
		// The worker drains the queue right away; Scout completes at once.
		for {
			job, err := refresher.Jobs.ClaimNext(ctx, jobqueue.JobQueueJevScout, *clock)
			if err != nil {
				break
			}
			if err := refresher.Jobs.MarkSucceeded(ctx, job.ID, *clock); err != nil {
				t.Fatalf("MarkSucceeded: %v", err)
			}
			scoutedAt = append(scoutedAt, *clock)
		}
	}

	if len(scoutedAt) != 3 {
		t.Fatalf("Scout ran %d times over 3 minutes (every 20s refresh), want 3: %v", len(scoutedAt), scoutedAt)
	}
	for i := 1; i < len(scoutedAt); i++ {
		if gap := scoutedAt[i].Sub(scoutedAt[i-1]); gap < time.Minute {
			t.Errorf("Scout runs %d and %d are %v apart, want >= 1m", i-1, i, gap)
		}
	}
}

// Issue #388: a symbol with an unprocessed jev-scout job (pending or
// running - Jev can take 21.5s, non-functional.md §2.2) is not enqueued
// again, however much time has passed.
func TestRefresh_DoesNotDuplicateOpenJevScoutJob(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		claim bool
	}{{"pending", false}, {"running", true}} {
		t.Run(tc.name, func(t *testing.T) {
			refresher, _, clock := newScoutCooldownRefresher(t)
			if err := refresher.Refresh(ctx); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if tc.claim {
				if _, err := refresher.Jobs.ClaimNext(ctx, jobqueue.JobQueueJevScout, *clock); err != nil {
					t.Fatalf("ClaimNext: %v", err)
				}
			}

			*clock = clock.Add(10 * time.Minute)
			if err := refresher.Refresh(ctx); err != nil {
				t.Fatalf("Refresh: %v", err)
			}

			jobs, err := refresher.Jobs.ListOpenOrFinishedSince(ctx, jobqueue.JobQueueJevScout, clock.Add(-time.Hour))
			if err != nil {
				t.Fatalf("ListOpenOrFinishedSince: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("jev-scout jobs = %d, want 1 (no duplicate while %s)", len(jobs), tc.name)
			}
		})
	}
}

// Issue #388: an FR-SCAN-1 event-driven jev-scout job (enqueued outside
// Refresh) holds the symbol back just like one Refresh enqueued itself.
func TestRefresh_SkipsSymbolWithEventDrivenJevScoutJob(t *testing.T) {
	refresher, inst, _ := newScoutCooldownRefresher(t)
	ctx := context.Background()
	payload := `{"instrument_id":` + strconv.FormatInt(inst.ID, 10) + `,"symbol":"7203"}`
	if _, err := refresher.Jobs.Enqueue(ctx, jobqueue.JobQueueJevScout, payload, time.Now().UTC()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := refresher.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	jobs, err := refresher.Jobs.ListOpenOrFinishedSince(ctx, jobqueue.JobQueueJevScout, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListOpenOrFinishedSince: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jev-scout jobs = %d, want 1 (the event-driven one only)", len(jobs))
	}
}

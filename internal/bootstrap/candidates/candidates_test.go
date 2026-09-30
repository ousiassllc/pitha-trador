package candidates

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func TestRefresh_PublishesTopScreenedInstrumentsToScreenerSource(t *testing.T) {
	refresher := newTestRefresher(t)
	// Override every screener.PassesFilter threshold to lenient values so
	// this test only depends on the Input this test builds, not on
	// config/strategy.yaml's own real (stricter) production thresholds.
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 100,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}

	inst := mustCreateInstrument(t, refresher, "7203")
	now := time.Now().UTC()

	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID,
		Symbol:       inst.Symbol,
		Timestamp:    now,
		Price:        2500,
		Volume:       1000,
		Turnover:     2_500_000,
		SpreadBps:    ptrF(10),
		Feature: domain.Feature{
			VWAP: 2490, PriceVsVWAPBps: 40,
			VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
		},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	candidates, asOf, err := refresher.Screener.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].Symbol != "7203" {
		t.Errorf("candidates[0].Symbol = %q, want %q", candidates[0].Symbol, "7203")
	}
	if candidates[0].Price != 2500 {
		t.Errorf("candidates[0].Price = %v, want 2500", candidates[0].Price)
	}
	if asOf.IsZero() {
		t.Error("asOf is zero, want the refresh time")
	}
}

func TestRefresh_EnqueuesJevScoutJobForEachCandidate(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.InSession = func(time.Time) bool { return true } // not wall-clock dependent
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 100,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}

	inst := mustCreateInstrument(t, refresher, "7203")
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{
			VWAP: 2490, PriceVsVWAPBps: 40,
			VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
		},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	job, err := refresher.Jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevScout, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("ClaimNext(jev-scout): %v, want one enqueued job", err)
	}
	if job.PayloadJSON == "" {
		t.Error("job.PayloadJSON is empty")
	}
}

func TestRefresh_SkipsInstrumentsWithNoSnapshotsYet(t *testing.T) {
	refresher := newTestRefresher(t)
	mustCreateInstrument(t, refresher, "9999") // no snapshots inserted

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	candidates, _, err := refresher.Screener.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("len(candidates) = %d, want 0 for an instrument with no snapshots", len(candidates))
	}
}

func TestCandidateRefreshInterval_NextStaysWithinMinMax(t *testing.T) {
	r := candidateRefreshInterval{min: 15 * time.Second, max: 30 * time.Second}
	for range 50 {
		d := r.next()
		if d < r.min || d > r.max {
			t.Fatalf("next() = %v, want within [%v, %v]", d, r.min, r.max)
		}
	}
}

func TestCandidateRefreshInterval_NextReturnsMinWhenMaxNotGreater(t *testing.T) {
	r := candidateRefreshInterval{min: 20 * time.Second, max: 20 * time.Second}
	if d := r.next(); d != r.min {
		t.Errorf("next() = %v, want %v when max <= min", d, r.min)
	}
}

// Issue #139: off-hours the candidate list still refreshes from stored
// data, but no Jev Scout job (a billed Jev call) is enqueued.
func TestRefresh_DoesNotEnqueueJevScoutOutsideSession(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.InSession = func(time.Time) bool { return false }
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000, MaxSpreadBps: 100, TopN: 10,
	}
	inst := mustCreateInstrument(t, refresher, "7203")
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{VWAP: 2490, PriceVsVWAPBps: 40, VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01)},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got, _, err := refresher.Screener.Candidates(context.Background()); err != nil || len(got) != 1 {
		t.Fatalf("candidates = %d, want 1 (list still refreshes off-hours)", len(got))
	}
	if _, err := refresher.Jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevScout, time.Now().UTC().Add(time.Second)); err == nil {
		t.Fatal("a jev-scout job was enqueued outside the trading session")
	}
}

package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func ptrF(v float64) *float64 { return &v }

func TestRefreshCandidates_PublishesTopScreenedInstrumentsToScreenerSource(t *testing.T) {
	svc := newTestServices(t, nil)
	// Override every screener.PassesFilter threshold to lenient values so
	// this test only depends on the Input this test builds, not on
	// config/strategy.yaml's own real (stricter) production thresholds.
	svc.strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 100,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}

	inst := mustCreateInstrument(t, svc, "7203")
	now := time.Now().UTC()

	if _, err := svc.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
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

	if err := svc.refreshCandidates(context.Background()); err != nil {
		t.Fatalf("refreshCandidates: %v", err)
	}

	candidates, asOf, err := svc.Screener.Candidates(context.Background())
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

func TestRefreshCandidates_EnqueuesJevScoutJobForEachCandidate(t *testing.T) {
	svc := newTestServices(t, nil)
	svc.strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 100,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}

	inst := mustCreateInstrument(t, svc, "7203")
	if _, err := svc.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{
			VWAP: 2490, PriceVsVWAPBps: 40,
			VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
		},
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if err := svc.refreshCandidates(context.Background()); err != nil {
		t.Fatalf("refreshCandidates: %v", err)
	}

	job, err := svc.Jobs.ClaimNext(context.Background(), repository.JobQueueJevScout, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("ClaimNext(jev-scout): %v, want one enqueued job", err)
	}
	if job.PayloadJSON == "" {
		t.Error("job.PayloadJSON is empty")
	}
}

func TestRefreshCandidates_SkipsInstrumentsWithNoSnapshotsYet(t *testing.T) {
	svc := newTestServices(t, nil)
	mustCreateInstrument(t, svc, "9999") // no snapshots inserted

	if err := svc.refreshCandidates(context.Background()); err != nil {
		t.Fatalf("refreshCandidates: %v", err)
	}

	candidates, _, err := svc.Screener.Candidates(context.Background())
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

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

func TestRefresh_RetainsPerSymbolScanResultsAndFunnel(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 100, MaxPrice: 1_000_000,
		MinTurnover5mJPY: 0, MaxSpreadBps: 50,
		MinVolumeRatio: 0, MinAbsReturn5mPct: 0, MinRealizedVolatility: 0,
		TopN: 10,
	}
	now := time.Now().UTC()
	insert := func(symbol string, price float64, spread *float64) {
		inst := mustCreateInstrument(t, refresher, symbol)
		if spread == nil && price == 0 { // no snapshot at all
			return
		}
		if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: now, Price: price, Turnover: 1_000,
			SpreadBps: spread,
			Feature:   domain.Feature{VolumeRatio5m: ptrF(1), Return5m: ptrF(1), RealizedVol5m: ptrF(0.01)},
		}}); err != nil {
			t.Fatalf("InsertBatch %s: %v", symbol, err)
		}
	}
	insert("1001", 1000, ptrF(10)) // passes
	insert("1002", 50, ptrF(10))   // min_price
	insert("1003", 1000, nil)      // no order book -> missing spread
	insert("1004", 0, nil)         // no snapshot yet

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	cycle, ok, err := refresher.Screener.Scan(context.Background())
	if err != nil || !ok {
		t.Fatalf("Scan() ok=%v err=%v", ok, err)
	}
	if cycle.Funnel.Universe != 4 || cycle.Funnel.FeatureComputed != 3 || cycle.Funnel.FastScreenerPassed != 1 {
		t.Errorf("funnel = %+v, want universe 4 / features 3 / fast 1", cycle.Funnel)
	}
	if cycle.StartedAt.IsZero() || cycle.FinishedAt.Before(cycle.StartedAt) {
		t.Errorf("cycle times = %v .. %v", cycle.StartedAt, cycle.FinishedAt)
	}
	if len(cycle.Symbols) != 4 {
		t.Fatalf("len(Symbols) = %d, want 4 (every universe instrument)", len(cycle.Symbols))
	}
	bySymbol := map[string]domain.ScanSymbol{}
	for _, s := range cycle.Symbols {
		bySymbol[s.Symbol] = s
	}
	if s := bySymbol["1001"]; s.Status() != domain.ScanStatusPassed || s.Name != "1001 Inc." || s.Market != "TSE Prime" {
		t.Errorf("1001 = %+v", s)
	}
	if s := bySymbol["1002"]; !s.Reasons.Has(domain.ScreenReasonMinPrice) || s.Status() != domain.ScanStatusExcluded {
		t.Errorf("1002 reasons = %v", s.Reasons.List())
	}
	if s := bySymbol["1003"]; !s.Reasons.Has(domain.ScreenReasonMissingSpread) || s.Status() != domain.ScanStatusMissing {
		t.Errorf("1003 reasons = %v", s.Reasons.List())
	}
	if s := bySymbol["1004"]; !s.Reasons.Has(domain.ScreenReasonNoSnapshot) || s.Status() != domain.ScanStatusMissing {
		t.Errorf("1004 reasons = %v", s.Reasons.List())
	}
}

func TestRefresh_ScanMarksUnknownTurnoverAsMissing(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.Strategy.FastScreener = config.FastScreenerConfig{MaxPrice: 1_000_000, MinTurnover5mJPY: 5_000_000, MaxSpreadBps: 50, TopN: 10}
	inst := mustCreateInstrument(t, refresher, "2001")
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(), Price: 1000, Turnover: 1,
		SpreadBps: ptrF(1), Feature: domain.Feature{VolumeRatio5m: ptrF(1), Return5m: ptrF(1), RealizedVol5m: ptrF(1)},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	cycle, _, _ := refresher.Screener.Scan(context.Background())
	got := cycle.Symbols[0].Reasons
	// One bar of history cannot give a 5-minute turnover: that is a data
	// gap (missing_turnover), not a below-the-floor reading.
	if !got.Has(domain.ScreenReasonMissingTurnover) || got.Has(domain.ScreenReasonMinTurnover) {
		t.Fatalf("reasons = %v, want only missing_turnover", got.List())
	}
}

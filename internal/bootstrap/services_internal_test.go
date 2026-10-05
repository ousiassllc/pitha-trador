package bootstrap

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// newTestServices builds a *Services backed by a fresh temp-dir SQLite DB.
// When kabuServer is non-nil, MarketData is rebound to it (mirroring
// internal/service/marketdata's own test-double pattern - client_test.go's
// httptest.NewServer usage) instead of the real kabuステーションAPI
// DefaultBaseURL BuildServices would otherwise use.
func newTestServices(t *testing.T) *Services {
	t.Helper()
	state, err := Run(Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	// Jev calls fail after one attempt (WithJevMaxAttempts below): tests here
	// only need a Handler to run, not the real 1.5s retry backoff (issue #236).

	// JevBaseURL points at a closed local port: an empty one would fall back
	// to the production host (issue #271) and make these tests dial it.
	svc := BuildServices(state, config.Secrets{KabuAPIPassword: "test-password", JevBaseURL: "http://127.0.0.1:1"}, WithJevMaxAttempts(1), WithExecutionClock(func() time.Time { return tradingHours.Add(time.Minute) }))
	return svc
}

func mustCreateInstrument(t *testing.T, svc *Services, symbol string) domain.Instrument {
	t.Helper()
	inst, err := svc.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

// TestBuildServices_RegistersFeatureCalcHandler proves BuildServices
// registered a real Handler for the feature-calc queue (not merely left
// it unregistered) by enqueuing directly onto that queue and letting
// Scheduler.Start's own worker goroutine claim/process it: this test
// only ever reads job status via the read-only Jobs.Get, never calling
// the mutating ClaimNext itself, so it cannot race the real worker for
// the claim (an earlier version of this test called ClaimNext in its own
// polling loop and non-deterministically stole the claim before the
// worker did, self-defeating the very thing it meant to prove).
func TestBuildServices_RegistersFeatureCalcHandler(t *testing.T) {
	svc := newTestServices(t)

	enqueued, err := svc.Jobs.Enqueue(context.Background(), jobqueue.JobQueueFeatureCalc, "{}", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer svc.Scheduler.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for {
		job, err := svc.Jobs.Get(context.Background(), enqueued.ID)
		if err != nil {
			t.Fatalf("Jobs.Get: %v", err)
		}
		if job.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("feature-calc job status = %q after 3s of Scheduler.Start; want %q (handler not registered/running)", job.Status, "succeeded")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBuildServices_RegistersMarketDataHandler proves BuildServices
// registered a real Handler for the market-data queue the same way: a job
// with an undecodable payload ends "failed" (a handler ran), not "pending"
// (an unregistered queue is never claimed).
func TestBuildServices_RegistersMarketDataHandler(t *testing.T) {
	svc := newTestServices(t)

	enqueued, err := svc.Jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, "not-json", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer svc.Scheduler.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for {
		job, err := svc.Jobs.Get(context.Background(), enqueued.ID)
		if err != nil {
			t.Fatalf("Jobs.Get: %v", err)
		}
		if job.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("market-data job status = %q after 3s of Scheduler.Start; want %q (handler not registered/running)", job.Status, "failed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBuildServices_RegistersJevScoutHandler proves BuildServices
// registered a real Handler for the jev-scout queue (issue #46), the
// same way TestBuildServices_RegistersFeatureCalcHandler proves it for
// feature-calc: enqueue directly, let the real worker claim/process it,
// and read the resulting status via the read-only Jobs.Get. No Jev API
// server is stood up here, so the job is expected to fail (a network
// error dialing the unreachable BaseURL newTestServices sets) rather than succeed - "failed"
// still proves a Handler ran (an unregistered queue's job would stay
// "pending" forever, per Scheduler.Start's own doc comment: it only
// spins up a worker per *registered* queue).
func TestBuildServices_RegistersJevScoutHandler(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")

	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enqueued, err := svc.Jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer svc.Scheduler.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := svc.Jobs.Get(context.Background(), enqueued.ID)
		if err != nil {
			t.Fatalf("Jobs.Get: %v", err)
		}
		if job.Status == "failed" || job.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jev-scout job status = %q after 10s of Scheduler.Start; want %q or %q (handler not registered/running)", job.Status, "failed", "succeeded")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBuildServices_RegistersJevTraderHandler proves BuildServices
// registered a real Handler for the jev-trader queue (issue #47), the
// same way TestBuildServices_RegistersJevScoutHandler proves it for
// jev-scout. policy.Handler.HandleJob requires at least one
// market_snapshots row for the instrument (it errors otherwise before
// ever calling Jev Trader), so this test inserts one first; the job
// itself is still expected to end up "failed" (no real Jev API server
// here either), which - same as the jev-scout test - is sufficient proof
// a Handler ran rather than the job staying "pending" forever.
func TestBuildServices_RegistersJevTraderHandler(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")
	if _, err := svc.Snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000,
		Feature: domain.Feature{VWAP: 2490, PriceVsVWAPBps: 40},
	}); err != nil {
		t.Fatalf("Snapshots.Insert: %v", err)
	}

	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enqueued, err := svc.Jobs.Enqueue(context.Background(), jobqueue.JobQueueJevTrader, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer svc.Scheduler.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := svc.Jobs.Get(context.Background(), enqueued.ID)
		if err != nil {
			t.Fatalf("Jobs.Get: %v", err)
		}
		if job.Status == "failed" || job.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jev-trader job status = %q after 10s of Scheduler.Start; want %q or %q (handler not registered/running)", job.Status, "failed", "succeeded")
		}
		time.Sleep(20 * time.Millisecond)
	}

	signals, err := svc.Signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("Signals.ListByInstrument: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1 (policy.Handler records a trade_signals row even on Jev Trader API error)", len(signals))
	}
	if signals[0].PolicyVersion != policy.Version {
		t.Errorf("signals[0].PolicyVersion = %q, want %q", signals[0].PolicyVersion, policy.Version)
	}
}

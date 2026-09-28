package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// newTestServices builds a *Services backed by a fresh temp-dir SQLite DB.
// When kabuServer is non-nil, MarketData is rebound to it (mirroring
// internal/service/marketdata's own test-double pattern - client_test.go's
// httptest.NewServer usage) instead of the real kabuステーションAPI
// DefaultBaseURL BuildServices would otherwise use.
func newTestServices(t *testing.T, kabuServer *httptest.Server) *Services {
	t.Helper()
	state, err := Run(Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	svc, err := BuildServices(state, config.Secrets{KabuAPIPassword: "test-password"}, nil)
	if err != nil {
		t.Fatalf("BuildServices: %v", err)
	}
	if kabuServer != nil {
		svc.MarketData = marketdata.NewClient(marketdata.Config{
			BaseURL:     kabuServer.URL,
			APIPassword: "test-password",
		})
	}
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

func kabuFakeServer(t *testing.T, board any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-test"})
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(board)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestHandleMarketData_FetchesComputesAndPersistsSnapshot(t *testing.T) {
	server := kabuFakeServer(t, map[string]any{
		"Symbol": "7203", "CurrentPrice": 2500.0, "VWAP": 2490.0,
		"TradingVolume": 1000000.0, "TradingValue": 2.49e9,
	})
	defer server.Close()

	svc := newTestServices(t, server)
	inst := mustCreateInstrument(t, svc, "7203")

	if _, err := svc.MarketData.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	payload, err := json.Marshal(marketDataJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	job := repository.Job{PayloadJSON: string(payload)}

	if err := svc.handleMarketData(context.Background(), job); err != nil {
		t.Fatalf("handleMarketData: %v", err)
	}

	snaps, err := svc.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	if snaps[0].Price != 2500.0 {
		t.Errorf("Price = %v, want 2500.0", snaps[0].Price)
	}
	if snaps[0].Feature.VWAP != 2490.0 {
		t.Errorf("Feature.VWAP = %v, want 2490.0", snaps[0].Feature.VWAP)
	}
}

func TestHandleMarketData_ReturnsErrorWithoutSwallowingOnFetchFailure(t *testing.T) {
	svc := newTestServices(t, nil) // no server: GetBoard has no token, ErrNoToken
	inst := mustCreateInstrument(t, svc, "9999")

	payload, _ := json.Marshal(marketDataJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	job := repository.Job{PayloadJSON: string(payload)}

	if err := svc.handleMarketData(context.Background(), job); err == nil {
		t.Fatal("handleMarketData: want error when kabuステーションAPI is unreachable, got nil")
	}

	snaps, err := svc.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("len(snaps) = %d, want 0 (no snapshot persisted on fetch failure)", len(snaps))
	}
}

func TestHandleMarketData_ReturnsErrorOnUnmarshalableJobPayload(t *testing.T) {
	svc := newTestServices(t, nil)
	job := repository.Job{PayloadJSON: "not-json"}

	if err := svc.handleMarketData(context.Background(), job); err == nil {
		t.Fatal("handleMarketData: want error for unmarshalable job payload, got nil")
	}
}

func TestHandleFeatureCalc_IsANoOp(t *testing.T) {
	svc := newTestServices(t, nil)
	if err := svc.handleFeatureCalc(context.Background(), repository.Job{PayloadJSON: "{}"}); err != nil {
		t.Errorf("handleFeatureCalc: %v, want nil", err)
	}
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
	svc := newTestServices(t, nil)

	enqueued, err := svc.Jobs.Enqueue(context.Background(), repository.JobQueueFeatureCalc, "{}", time.Now().UTC())
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

// TestBuildServices_RegistersJevScoutHandler proves BuildServices
// registered a real Handler for the jev-scout queue (issue #46), the
// same way TestBuildServices_RegistersFeatureCalcHandler proves it for
// feature-calc: enqueue directly, let the real worker claim/process it,
// and read the resulting status via the read-only Jobs.Get. No Jev API
// server is stood up here, so the job is expected to fail (a network
// error dialing the empty/invalid BaseURL) rather than succeed - "failed"
// still proves a Handler ran (an unregistered queue's job would stay
// "pending" forever, per Scheduler.Start's own doc comment: it only
// spins up a worker per *registered* queue).
func TestBuildServices_RegistersJevScoutHandler(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")

	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enqueued, err := svc.Jobs.Enqueue(context.Background(), repository.JobQueueJevScout, string(payload), time.Now().UTC())
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
		if job.Status == "failed" || job.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jev-scout job status = %q after 3s of Scheduler.Start; want %q or %q (handler not registered/running)", job.Status, "failed", "succeeded")
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
	svc := newTestServices(t, nil)
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
	enqueued, err := svc.Jobs.Enqueue(context.Background(), repository.JobQueueJevTrader, string(payload), time.Now().UTC())
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
		if job.Status == "failed" || job.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jev-trader job status = %q after 3s of Scheduler.Start; want %q or %q (handler not registered/running)", job.Status, "failed", "succeeded")
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

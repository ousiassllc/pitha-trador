package startup_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// newServices builds Services on a fresh temp DB whose kabuステーションAPI
// answers every token request with a 500 (so MarketData.Start's initial
// token issuance fails deterministically) and whose Jev points at a closed
// local port, so nothing here dials a real host.
func newServices(t *testing.T) *bootstrap.Services {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	svc := bootstrap.BuildServices(state, config.Secrets{KabuAPIPassword: "test-password", JevBaseURL: "http://127.0.0.1:1"}, bootstrap.WithJevMaxAttempts(1))
	kabu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(kabu.Close)
	svc.MarketData = marketdata.NewClient(marketdata.Config{BaseURL: kabu.URL + "/kabusapi", APIPassword: "test-password"})
	return svc
}

// A failed initial kabu token must not fail Start, and Start -> ctx cancel
// -> Stop must return in bounded time with every goroutine Start launched
// (candidate refresh, held-position monitor, PUSH feed, the Scheduler's
// workers) gone: a missed wg.Done or a goroutine ignoring ctx would hang Stop.
func TestServices_StartToleratesTokenFailureAndStopReturnsAfterCancel(t *testing.T) {
	svc := newServices(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start with a failing kabu token: err = %v, want nil", err)
	}
	if _, held := svc.MarketData.Token(); held {
		t.Fatal("kabu token held after a 500 token response; the test double is not failing")
	}

	cancel()
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return within 10s after the Start ctx was canceled")
	}
}

// Recover must run before the Scheduler starts: a job a previous process
// left 'running' is only claimable once Recover has reset it. A Start whose
// Recover fails (canceled ctx) must not have launched or recovered anything.
func TestServices_StartRecoversStuckJobsBeforeStartingScheduler(t *testing.T) {
	svc := newServices(t)
	bg := context.Background()
	stuck, err := svc.Jobs.Enqueue(bg, jobqueue.JobQueueFeatureCalc, "{}", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := svc.Jobs.ClaimNext(bg, jobqueue.JobQueueFeatureCalc, time.Now().UTC()); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	canceled, cancelNow := context.WithCancel(bg)
	cancelNow()
	if err := svc.Start(canceled); err == nil {
		t.Fatal("Start with a canceled ctx: err = nil, want the Recover failure")
	}
	if job, err := svc.Jobs.Get(bg, stuck.ID); err != nil || job.Status != jobqueue.JobStatusRunning {
		t.Fatalf("after a failed Start: job = %+v, err = %v; want status %q (nothing may have run)", job, err, jobqueue.JobStatusRunning)
	}

	ctx, cancel := context.WithCancel(bg)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()
	defer cancel()

	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := svc.Jobs.Get(bg, stuck.ID)
		if err != nil {
			t.Fatalf("Jobs.Get: %v", err)
		}
		if job.Status == jobqueue.JobStatusSucceeded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovered job status = %q after 5s, want %q", job.Status, jobqueue.JobStatusSucceeded)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

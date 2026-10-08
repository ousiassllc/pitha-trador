package tokenflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
)

// rotatingServer mimics kabuステーション: /token always issues a new token
// and invalidates the previous one; every other endpoint answers 401 /
// 4001009 unless X-API-KEY is the current token (issue #621).
type rotatingServer struct {
	*httptest.Server
	mu         sync.Mutex
	current    string
	issued     int
	tokenCalls atomic.Int64
	// failIssue makes /token answer 400 / 4001013 (reissue impossible).
	failIssue atomic.Bool
}

func newRotatingServer(t *testing.T) *rotatingServer {
	t.Helper()
	s := &rotatingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			s.tokenCalls.Add(1)
			if s.failIssue.Load() {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001013, "Message": "bad password"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": s.rotate()})
			return
		}
		s.mu.Lock()
		ok := r.Header.Get("X-API-KEY") == s.current
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001009, "Message": "api key mismatch"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": "7203", "CurrentPrice": 2409.0})
	}))
	t.Cleanup(s.Close)
	return s
}

// rotate invalidates the current token and returns the newly issued one.
func (s *rotatingServer) rotate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issued++
	s.current = "tok" + string(rune('0'+s.issued))
	return s.current
}

func TestClient_GetBoard_ReissuesRevokedToken(t *testing.T) {
	server := newRotatingServer(t)
	ctx := context.Background()
	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard before revocation: %v", err)
	}

	server.rotate() // kabuステーション re-login: the held token is now invalid
	before := server.tokenCalls.Load()

	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard after revocation: %v", err)
	}
	if got := server.tokenCalls.Load() - before; got != 1 {
		t.Errorf("/token calls = %d, want 1", got)
	}
	if held, _ := client.Token(); held != server.current {
		t.Errorf("held token = %q, want the reissued %q", held, server.current)
	}
	if healthy, _ := (broker.MarketDataChecker{Health: client}).Healthy(ctx); !healthy {
		t.Error("Healthy() = false after a recovered revocation")
	}
	if client.Status().IsStale("7203") {
		t.Error("symbol stale although the retried GetBoard succeeded")
	}
	if status := client.TokenStatus(); status.Failed() {
		t.Errorf("TokenStatus = %+v, want no failure after reissue", status)
	}

	// A healthy follow-up call must not reissue again.
	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard after recovery: %v", err)
	}
	if got := server.tokenCalls.Load() - before; got != 1 {
		t.Errorf("/token calls after follow-up = %d, want 1", got)
	}
}

func TestClient_GetBoard_ConcurrentRevokedCallsShareOneReissue(t *testing.T) {
	server := newRotatingServer(t)
	ctx := context.Background()
	client := marketdata.NewClient(marketdata.Config{
		BaseURL: server.URL, APIPassword: "pw", InfoAPIMaxPerSecond: 10,
		Clock: infolimit.NewManualClock(time.Time{}),
	})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	server.rotate()
	before := server.tokenCalls.Load()

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("GetBoard: %v", err)
		}
	}
	if got := server.tokenCalls.Load() - before; got != 1 {
		t.Errorf("/token calls = %d, want 1 for %d concurrent callers", got, callers)
	}
}

func TestClient_GetBoard_FailedReissueReturnsOriginalError(t *testing.T) {
	server := newRotatingServer(t)
	ctx := context.Background()
	clock := infolimit.NewManualClock(time.Time{})
	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw", Clock: clock})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	server.rotate()
	server.failIssue.Store(true)
	before := server.tokenCalls.Load()

	_, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE)
	var apiErr *marketdata.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized || apiErr.Code != 4001009 {
		t.Fatalf("GetBoard err = %v, want the original 401 / 4001009 APIError", err)
	}
	if !client.Status().IsStale("7203") {
		t.Error("symbol not marked stale after the unrecovered failure")
	}

	// Rejected calls within the throttle window must not hammer /token.
	for range 4 {
		if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err == nil {
			t.Fatal("GetBoard unexpectedly succeeded")
		}
	}
	if got := server.tokenCalls.Load() - before; got != 1 {
		t.Errorf("/token calls = %d, want 1 within the throttle window", got)
	}
	if healthy, _ := (broker.MarketDataChecker{Health: client}).Healthy(ctx); healthy {
		t.Error("Healthy() = true after 5 unrecovered 401s, want false")
	}

	// Once the window passes and /token works again, the client recovers.
	server.failIssue.Store(false)
	<-clock.After(time.Minute) // ManualClock.After advances Now
	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard after the throttle window: %v", err)
	}
	if healthy, _ := (broker.MarketDataChecker{Health: client}).Healthy(ctx); !healthy {
		t.Error("Healthy() = false after recovery")
	}
}

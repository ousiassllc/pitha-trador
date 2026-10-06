package tokenflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
)

// refusingServer issues tokens happily but answers every information API
// with 401 / 4001007 until accept is set: the "logged in, yet rejected"
// state seen with a full scan failing symbol by symbol.
type refusingServer struct {
	*httptest.Server
	infoCalls atomic.Int64
	accept    atomic.Bool
}

func newRefusingServer(t *testing.T) *refusingServer {
	t.Helper()
	s := &refusingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
			return
		}
		s.infoCalls.Add(1)
		if !s.accept.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001007, "Message": "ログイン認証エラー"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": "7203", "CurrentPrice": 2409.0})
	}))
	t.Cleanup(s.Close)
	return s
}

// While even a freshly issued token is rejected, further calls fail fast
// without reaching kabu station, the banner status says so, and one probe
// per cooldown detects recovery.
func TestAuthBreaker_FailsFastWhileFreshTokenIsRejected(t *testing.T) {
	server := newRefusingServer(t)
	ctx := context.Background()
	clock := infolimit.NewManualClock(time.Time{})
	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw", Clock: clock})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err == nil {
		t.Fatal("GetBoard unexpectedly succeeded")
	}
	tripped := server.infoCalls.Load() // original request + retry with the fresh token
	for range 50 {
		if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err == nil {
			t.Fatal("GetBoard unexpectedly succeeded while the breaker is open")
		}
	}
	if got := server.infoCalls.Load(); got != tripped {
		t.Errorf("info API calls while open = %d, want %d (fail fast)", got, tripped)
	}
	if got := client.TokenStatus(); got.Issue != marketdata.TokenIssueRejected || got.Code != 4001007 {
		t.Errorf("TokenStatus = %+v, want rejected / 4001007", got)
	}

	// After the cooldown exactly one call probes; once accepted, everything resumes.
	server.accept.Store(true)
	<-clock.After(time.Minute)
	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard after the cooldown: %v", err)
	}
	if got := client.TokenStatus(); got.Failed() {
		t.Errorf("TokenStatus after recovery = %+v, want cleared", got)
	}
}

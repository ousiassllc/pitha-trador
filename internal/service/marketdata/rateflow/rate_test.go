package rateflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}

func tokenAndBoardServer(t *testing.T, onBoard func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
			return
		}
		onBoard(w, r)
	}))
}

func issuedClient(t *testing.T, server *httptest.Server, clock infolimit.Clock, maxPerSec int) *marketdata.Client {
	t.Helper()
	client := marketdata.NewClient(marketdata.Config{
		BaseURL:             server.URL,
		APIPassword:         "secret",
		InfoAPIMaxPerSecond: maxPerSec,
		Clock:               clock,
	})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return client
}

func writeOKBoard(w http.ResponseWriter, symbol string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": symbol, "CurrentPrice": 2409.0})
}

func writeRateLimit(w http.ResponseWriter) {
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{"Code": marketdata.CodeAPIRateLimit, "Message": "API実行回数エラー"})
}

// A 2000-symbol REST scan at 8 req/s must stay at or under the official
// 10/s cap (fake clock, fake client). Issue #514.
func TestClient_GetBoard_RespectsInfoAPIRateCap(t *testing.T) {
	var calls atomic.Int32
	server := tokenAndBoardServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeOKBoard(w, "7203")
	})
	defer server.Close()

	clock := infolimit.NewManualClock(time.Time{})
	client := issuedClient(t, server, clock, 8)
	ctx := context.Background()

	const n = 2000
	stamps := make([]time.Time, 0, n)
	start := clock.Now()
	for i := range n {
		if _, err := client.GetBoard(ctx, fmt.Sprintf("%04d", i), marketdata.ExchangeTSE); err != nil {
			t.Fatalf("GetBoard[%d]: %v", i, err)
		}
		stamps = append(stamps, clock.Now())
	}

	if got := int(calls.Load()); got != n {
		t.Fatalf("HTTP board calls = %d, want %d", got, n)
	}
	if got := maxInWindow(stamps, time.Second); got > 8 {
		t.Fatalf("max GetBoard in any 1s window = %d, want <= 8", got)
	}
	// 2000 grants at 8/s need at least (2000-8)/8 = 249 seconds of fake time.
	if elapsed := stamps[n-1].Sub(start); elapsed < 249*time.Second {
		t.Fatalf("2000 GetBoard spanned %s, want at least 249s at 8/s", elapsed)
	}
	if client.RateLimitStats().Overflows != 0 {
		t.Fatalf("overflows = %d, want 0 (limiter should prevent 4001006)", client.RateLimitStats().Overflows)
	}
}

func TestClient_GetBoard_Retries4001006ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := tokenAndBoardServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			writeRateLimit(w)
			return
		}
		writeOKBoard(w, "7203")
	})
	defer server.Close()

	clock := infolimit.NewManualClock(time.Time{})
	client := issuedClient(t, server, clock, 8)
	if _, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("board calls = %d, want 3 (2×4001006 then success)", got)
	}
	if got := client.RateLimitStats().Overflows; got != 2 {
		t.Fatalf("overflows = %d, want 2", got)
	}
	if client.Status().IsStale("7203") {
		t.Fatal("symbol marked stale after a recovered 4001006")
	}
	ok, err := client.Healthy(context.Background())
	if err != nil || !ok {
		t.Fatalf("Healthy = (%v, %v), want true (4001006 is not market_data_down)", ok, err)
	}
}

func TestClient_GetBoard_Exhausted4001006IsErrRateLimited(t *testing.T) {
	var calls atomic.Int32
	server := tokenAndBoardServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeRateLimit(w)
	})
	defer server.Close()

	clock := infolimit.NewManualClock(time.Time{})
	client := issuedClient(t, server, clock, 8)
	_, err := client.GetBoard(context.Background(), "9997", marketdata.ExchangeTSE)
	if !errors.Is(err, marketdata.ErrRateLimited) {
		t.Fatalf("GetBoard err = %v, want ErrRateLimited", err)
	}
	if !marketdata.IsRateLimit(err) {
		t.Fatalf("IsRateLimit(%v) = false, want true (wrapped APIError)", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("board calls = %d, want 4 (initial + 3 retries)", got)
	}
	if status, ok := client.Status().Status("9997"); ok {
		t.Fatalf("ErrRateLimited recorded status %+v, want no stale mark", status)
	}
	ok, herr := client.Healthy(context.Background())
	if herr != nil || !ok {
		t.Fatalf("Healthy = (%v, %v), want true", ok, herr)
	}
}

func TestClient_GetSymbolAndRegisterShareTheSameLimiter(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/register":
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"RegistList": []any{}})
		default:
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": "7203"})
		}
	}))
	defer server.Close()

	clock := infolimit.NewManualClock(time.Time{})
	client := issuedClient(t, server, clock, 8)
	ctx := context.Background()
	stamps := make([]time.Time, 0, 16)
	start := clock.Now()
	for i := range 8 {
		if _, err := client.GetBoard(ctx, fmt.Sprintf("b%d", i), marketdata.ExchangeTSE); err != nil {
			t.Fatalf("GetBoard: %v", err)
		}
		stamps = append(stamps, clock.Now())
	}
	if _, err := client.GetSymbol(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetSymbol: %v", err)
	}
	stamps = append(stamps, clock.Now())
	if _, err := client.RegisterSymbols(ctx, nil); err != nil {
		t.Fatalf("RegisterSymbols: %v", err)
	}
	stamps = append(stamps, clock.Now())
	if got := maxInWindow(stamps, time.Second); got > 8 {
		t.Fatalf("mixed info/register calls in any 1s window = %d, want <= 8", got)
	}
	if stamps[len(stamps)-1].Equal(start) {
		t.Fatal("9th+ info/register call did not wait for a new window")
	}
}

func maxInWindow(stamps []time.Time, window time.Duration) int {
	maxN := 0
	for i, t := range stamps {
		n := 0
		cutoff := t.Add(-window)
		for _, s := range stamps[:i+1] {
			if s.After(cutoff) {
				n++
			}
		}
		if n > maxN {
			maxN = n
		}
	}
	return maxN
}

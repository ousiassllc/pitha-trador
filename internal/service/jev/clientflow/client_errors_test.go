package clientflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

func TestClient_Scout_InvalidResponseIsNotRetried(t *testing.T) {
	noul := func(v float64) map[string]any { return jevtest.Noul(v) }
	valid := func() map[string]any {
		return map[string]any{
			"interesting_now":   noul(0.8),
			"momentum_quality":  jevtest.Choice("strong", 0.7),
			"liquidity_ok":      noul(0.9),
			"abnormal_activity": noul(0.6),
		}
	}
	with := func(id string, answer map[string]any) map[string]any {
		answers := valid()
		if answer == nil {
			delete(answers, id)
		} else {
			answers[id] = answer
		}
		return answers
	}

	tests := map[string]map[string]any{
		"missing answer":              with("liquidity_ok", nil),
		"noul answered as choice":     with("interesting_now", jevtest.Choice("strong", 0.7)),
		"choice answered as noul":     with("momentum_quality", noul(0.5)),
		"choice outside options":      with("momentum_quality", jevtest.Choice("LONG", 0.7)),
		"choice missing value":        with("momentum_quality", map[string]any{"type": "choice", "confidence": 0.7}),
		"noul above 1":                with("abnormal_activity", noul(1.2)),
		"noul below 0":                with("abnormal_activity", noul(-0.1)),
		"noul value missing":          with("liquidity_ok", map[string]any{"type": "noul"}),
		"choice confidence out range": with("momentum_quality", jevtest.Choice("strong", 1.5)),
	}
	for name, answers := range tests {
		t.Run(name, func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				_, _ = w.Write(jevtest.Response("jev-1.13.0", answers))
			}))
			defer server.Close()

			client := jev.NewClient(jev.Config{BaseURL: server.URL, RetryBaseDelay: time.Millisecond})
			_, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
			if !errors.Is(err, systemone.ErrInvalidResponse) {
				t.Fatalf("Scout error = %v, want systemone.ErrInvalidResponse", err)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("server received %d calls, want 1 (an invalid response must not be retried)", got)
			}
		})
	}
}

func TestClient_Trader_InvalidResponseIsNotRetried(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{
			"direction":                jevtest.Choice("LONG", 0.7),
			"regime":                   jevtest.Choice("TREND", 0.7),
			"entry_quality":            jevtest.Choice("good", 0.7),
			"toxic_flow":               jevtest.Noul(0.1),
			"liquidity_stressed":       jevtest.Noul(0.1),
			"continuation_probability": jevtest.Noul(0.6),
		}
	}
	tests := map[string]func(map[string]any){
		"missing direction":     func(a map[string]any) { delete(a, "direction") },
		"direction not defined": func(a map[string]any) { a["direction"] = jevtest.Choice("BUY", 0.7) },
		"regime not defined":    func(a map[string]any) { a["regime"] = jevtest.Choice("strong", 0.7) },
		"entry_quality wrong":   func(a map[string]any) { a["entry_quality"] = jevtest.Choice("TREND", 0.7) },
		"noul out of range":     func(a map[string]any) { a["toxic_flow"] = jevtest.Noul(2) },
		"noul type mismatch":    func(a map[string]any) { a["continuation_probability"] = jevtest.Choice("NONE", 0.5) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			answers := valid()
			mutate(answers)
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				_, _ = w.Write(jevtest.Response("jev-1.13.0", answers))
			}))
			defer server.Close()

			client := jev.NewClient(jev.Config{BaseURL: server.URL, RetryBaseDelay: time.Millisecond})
			_, _, err := client.Trader(context.Background(), jev.TraderRequest{})
			if !errors.Is(err, systemone.ErrInvalidResponse) {
				t.Fatalf("Trader error = %v, want systemone.ErrInvalidResponse", err)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("server received %d calls, want 1", got)
			}
		})
	}
}

func TestClient_Scout_MalformedBodyIsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":`))
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL})
	if _, _, err := client.Scout(context.Background(), jev.ScoutRequest{}); !errors.Is(err, systemone.ErrInvalidResponse) {
		t.Fatalf("Scout error = %v, want systemone.ErrInvalidResponse", err)
	}
}

// 401 (bad key) and 422 (schema violation) cannot be fixed by repeating
// the request: fail fast with the APIError.
func TestClient_NonRetryableStatusFailsFast(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"detail":"nope"}`))
			}))
			defer server.Close()

			client := jev.NewClient(jev.Config{BaseURL: server.URL, RetryBaseDelay: time.Millisecond})
			_, _, err := client.Trader(context.Background(), jev.TraderRequest{})
			var apiErr *jev.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("Trader error = %v, want *jev.APIError with status %d", err, status)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("server received %d calls, want 1 (not retried)", got)
			}
		})
	}
}

// 429 and 529 are retried, but with backoff from the very first retry
// (unlike 5xx whose first retry is immediate).
func TestClient_ThrottledStatusBacksOffBeforeFirstRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var mu sync.Mutex
			var stamps []time.Time
			ok := jevtest.ScoutHandler(jev.ScoutResponse{InterestingNow: 0.9})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				stamps = append(stamps, time.Now())
				n := len(stamps)
				mu.Unlock()
				if n == 1 {
					w.WriteHeader(status)
					return
				}
				ok(w, r)
			}))
			defer server.Close()

			baseDelay := 40 * time.Millisecond
			client := jev.NewClient(jev.Config{BaseURL: server.URL, RetryBaseDelay: baseDelay})
			resp, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
			if err != nil {
				t.Fatalf("Scout: %v", err)
			}
			if resp.InterestingNow != 0.9 {
				t.Errorf("Scout() = %+v, want the retried success", resp)
			}
			if len(stamps) != 2 {
				t.Fatalf("got %d attempts, want 2", len(stamps))
			}
			if gap := stamps[1].Sub(stamps[0]); gap < baseDelay {
				t.Errorf("gap before the retry = %v, want at least the backoff base delay %v", gap, baseDelay)
			}
		})
	}
}

func TestClient_ThrottledStatusGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 3, RetryBaseDelay: time.Millisecond})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	var apiErr *jev.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("Scout error = %v, want *jev.APIError 429", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server received %d calls, want MaxAttempts=3", got)
	}
}

// A failed call (invalid response included) still counts towards the
// rolling error rate that drives Healthy.
func TestClient_InvalidResponseCountsTowardsErrorRate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jevtest.Response("jev-1.13.0", map[string]any{}))
	}))
	defer server.Close()

	alerts := &fakeAlertNotifier{}
	client := jev.NewClient(jev.Config{BaseURL: server.URL, Alerts: alerts, ErrorRateWindow: 5, ErrorRateThreshold: 0.5})
	for range 5 {
		_, _, _ = client.Scout(context.Background(), jev.ScoutRequest{})
	}
	if got := alerts.count(); got != 1 {
		t.Errorf("alert notifications = %d, want 1 after 5/5 invalid responses", got)
	}
	if healthy, _ := client.Healthy(context.Background()); healthy {
		t.Error("Healthy() = true, want false after the error rate breached")
	}
}

package assist_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// callRecorder records the arrival time of each request.
type callRecorder struct {
	mu    sync.Mutex
	times []time.Time
}

func (r *callRecorder) record() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.times = append(r.times, time.Now())
}

func (r *callRecorder) gaps() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []time.Duration
	for i := 1; i < len(r.times); i++ {
		out = append(out, r.times[i].Sub(r.times[i-1]))
	}
	return out
}

func (r *callRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.times)
}

func retryClient(t *testing.T, handler http.HandlerFunc, base time.Duration) (*assist.Client, *callRecorder) {
	t.Helper()
	rec := &callRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record()
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return assist.NewClient(assist.Config{Label: "test", BaseURL: server.URL, MaxAttempts: 4, RetryBaseDelay: base}), rec
}

func TestClient_PostJSON_DoesNotRetryUnrecoverableStatuses(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, rec := retryClient(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", status) }, time.Millisecond)
			err := client.PostJSON(context.Background(), "/x", map[string]string{}, &struct{}{})
			var apiErr *assist.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("err = %v, want APIError %d", err, status)
			}
			if rec.calls() != 1 {
				t.Errorf("calls = %d, want 1 (no retry)", rec.calls())
			}
		})
	}
}

func TestClient_PostJSON_RetriesServerErrorsUpToMaxAttempts(t *testing.T) {
	const base = 200 * time.Millisecond
	client, rec := retryClient(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusBadGateway) }, base)
	if err := client.PostJSON(context.Background(), "/x", map[string]string{}, &struct{}{}); err == nil {
		t.Fatal("PostJSON succeeded, want an error")
	}
	if rec.calls() != 4 {
		t.Fatalf("calls = %d, want MaxAttempts (4)", rec.calls())
	}
	gaps := rec.gaps()
	if gaps[0] >= base {
		t.Errorf("first retry gap = %v, want immediate (< %v)", gaps[0], base)
	}
	if gaps[1] < base || gaps[2] < 2*base {
		t.Errorf("backoff gaps = %v, want >= %v then >= %v", gaps[1:], base, 2*base)
	}
}

func TestClient_PostJSON_ThrottledStatusesBackOffFromFirstRetry(t *testing.T) {
	const base = 60 * time.Millisecond
	for _, status := range []int{http.StatusTooManyRequests, 529} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, rec := retryClient(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "slow down", status) }, base)
			if err := client.PostJSON(context.Background(), "/x", map[string]string{}, &struct{}{}); err == nil {
				t.Fatal("PostJSON succeeded, want an error")
			}
			if rec.calls() != 4 {
				t.Fatalf("calls = %d, want MaxAttempts (4)", rec.calls())
			}
			gaps := rec.gaps()
			for i, want := range []time.Duration{base, 2 * base, 4 * base} {
				if gaps[i] < want {
					t.Errorf("gap[%d] = %v, want >= %v", i, gaps[i], want)
				}
			}
		})
	}
}

func TestClient_PostJSON_InvalidJSONIsNotRetried(t *testing.T) {
	client, rec := retryClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{not json")) }, time.Millisecond)
	err := client.PostJSON(context.Background(), "/x", map[string]string{}, &struct{}{})
	if !errors.Is(err, assist.ErrInvalidResponse) {
		t.Fatalf("err = %v, want ErrInvalidResponse", err)
	}
	if rec.calls() != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", rec.calls())
	}
}

func TestClient_PostJSON_OversizedResponseIsNotRetried(t *testing.T) {
	const limit = 1 << 20
	client, rec := retryClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"pad":"` + strings.Repeat("a", limit) + `"}`))
	}, time.Millisecond)
	err := client.PostJSON(context.Background(), "/x", map[string]string{}, &struct{}{})
	if !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("err = %v, want httpbody.ErrTooLarge", err)
	}
	if rec.calls() != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", rec.calls())
	}
}

func TestClient_PostJSON_ResponseExactlyAtLimitIsRead(t *testing.T) {
	const limit = 1 << 20
	const prefix, suffix = `{"pad":"`, `"}`
	client, _ := retryClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(prefix + strings.Repeat("a", limit-len(prefix)-len(suffix)) + suffix))
	}, time.Millisecond)
	var out struct{ Pad string }
	if err := client.PostJSON(context.Background(), "/x", map[string]string{}, &out); err != nil {
		t.Fatalf("PostJSON at the exact limit: %v", err)
	}
	if len(out.Pad) != limit-len(prefix)-len(suffix) {
		t.Errorf("pad length = %d, want the full body", len(out.Pad))
	}
}

func TestLuna_Classify_OversizedResponseSurfacesErrTooLarge(t *testing.T) {
	var calls int
	luna := lunaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(strings.Repeat("a", 1<<20+1)))
	})
	if _, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"}); !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("err = %v, want httpbody.ErrTooLarge", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

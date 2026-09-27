package jev_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// fakeAlertNotifier records every JevAPIErrorRateExceeded call.
type fakeAlertNotifier struct {
	mu    sync.Mutex
	calls []struct{ rate, threshold float64 }
}

func (f *fakeAlertNotifier) JevAPIErrorRateExceeded(_ context.Context, rate, threshold float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ rate, threshold float64 }{rate, threshold})
	return nil
}

func (f *fakeAlertNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// scriptedServer replays results in order (true = respond 500, false =
// respond 200 with a minimal valid ScoutResponse), looping the last
// entry once exhausted.
func scriptedServer(t *testing.T, fail []bool) *httptest.Server {
	t.Helper()
	i := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shouldFail := fail[i]
		if i < len(fail)-1 {
			i++
		}
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"forced failure"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(jev.ScoutResponse{InterestingNow: 0.8, ModelID: "test"})
	}))
}

func TestClient_ErrorRateTracker_NotifiesOnceWhenThresholdBreached(t *testing.T) {
	// errorRateTracker starts evaluating once it has minErrorRateSamples
	// (5) calls: at call 5 the window is [T,F,T,F,T] (rate 60%), which
	// already exceeds the 50% threshold and fires the one alert this
	// test expects; every later call in this window stays >=50%, so no
	// further ("newly breached") alert fires.
	fails := []bool{true, false, true, false, true, true}
	server := scriptedServer(t, fails)
	defer server.Close()

	alerts := &fakeAlertNotifier{}
	client := jev.NewClient(jev.Config{
		BaseURL:            server.URL,
		MaxAttempts:        1, // one HTTP call per Scout() call, deterministic outcomes
		Alerts:             alerts,
		ErrorRateWindow:    6,
		ErrorRateThreshold: 0.5,
	})

	for range fails {
		_, _, _ = client.Scout(context.Background(), jev.ScoutRequest{QuestionVersion: "v1"})
	}

	if got := alerts.count(); got != 1 {
		t.Fatalf("alert notifications = %d, want 1 (notify once per breach episode)", got)
	}
}

func TestClient_ErrorRateTracker_DoesNotNotifyBelowThreshold(t *testing.T) {
	fails := []bool{true, false, false, false, false, false}
	server := scriptedServer(t, fails)
	defer server.Close()

	alerts := &fakeAlertNotifier{}
	client := jev.NewClient(jev.Config{
		BaseURL:            server.URL,
		MaxAttempts:        1,
		Alerts:             alerts,
		ErrorRateWindow:    6,
		ErrorRateThreshold: 0.5,
	})

	for range fails {
		_, _, _ = client.Scout(context.Background(), jev.ScoutRequest{QuestionVersion: "v1"})
	}

	if got := alerts.count(); got != 0 {
		t.Fatalf("alert notifications = %d, want 0 (1/6 stays below the 50%% threshold)", got)
	}
}

func TestClient_ErrorRateTracker_RenotifiesAfterRecoveryAndReBreach(t *testing.T) {
	// Verified call-by-call against errorRateTracker.record's sliding
	// window (size 6, minErrorRateSamples 5, threshold 0.5): breach #1
	// fires at call 5 ([T,T,T,T,F], rate 80%), the window recovers below
	// 50% by call 8 once enough of the early failures slide out, and
	// breach #2 fires at call 15 once three of the trailing six calls
	// turn to failures again ([F,F,F,T,T,T], rate 50%).
	fails := []bool{
		true, true, true, true, false, false,
		false, false, false, false, false, false,
		true, true, true, true, true, true,
	}
	server := scriptedServer(t, fails)
	defer server.Close()

	alerts := &fakeAlertNotifier{}
	client := jev.NewClient(jev.Config{
		BaseURL:            server.URL,
		MaxAttempts:        1,
		Alerts:             alerts,
		ErrorRateWindow:    6,
		ErrorRateThreshold: 0.5,
	})

	for range fails {
		_, _, _ = client.Scout(context.Background(), jev.ScoutRequest{QuestionVersion: "v1"})
	}

	if got := alerts.count(); got != 2 {
		t.Fatalf("alert notifications = %d, want 2 (one per breach episode)", got)
	}
}

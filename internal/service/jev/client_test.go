package jev_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

func TestClient_Scout_SucceedsOnFirstAttempt(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(jev.ScoutResponse{
			InterestingNow: 0.8, MomentumQuality: jev.MomentumQualityStrong,
			LiquidityOk: 0.9, AbnormalActivity: 0.6, ModelID: "jev-scout-test",
		})
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL})
	resp, _, err := client.Scout(context.Background(), jev.ScoutRequest{QuestionVersion: "scout-v1"})
	if err != nil {
		t.Fatalf("Scout: %v", err)
	}
	if resp.InterestingNow != 0.8 || resp.ModelID != "jev-scout-test" {
		t.Fatalf("Scout() = %+v, want the server's response decoded", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("server received %d calls, want 1 (no retry on success)", got)
	}
}

func TestClient_Scout_RetriesAndEventuallySucceeds(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(jev.ScoutResponse{InterestingNow: 0.7, LiquidityOk: 0.7, AbnormalActivity: 0.7})
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, RetryBaseDelay: time.Millisecond})
	resp, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	if err != nil {
		t.Fatalf("Scout: %v", err)
	}
	if resp.InterestingNow != 0.7 {
		t.Fatalf("Scout() = %+v, want the eventual successful response", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server received %d calls, want 3 (2 failures + 1 success)", got)
	}
}

func TestClient_Scout_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 3, RetryBaseDelay: time.Millisecond})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	if err == nil {
		t.Fatal("Scout: want error once every attempt fails, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server received %d calls, want exactly MaxAttempts=3 (継続失敗でnew entry停止: no further attempts)", got)
	}
}

func TestClient_Scout_FirstRetryIsImmediateSecondOnwardBackoff(t *testing.T) {
	var timestamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	baseDelay := 40 * time.Millisecond
	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 3, RetryBaseDelay: baseDelay})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	if err == nil {
		t.Fatal("Scout: want error, got nil")
	}
	if len(timestamps) != 3 {
		t.Fatalf("got %d attempts, want 3", len(timestamps))
	}

	// overview.md §6: "1回目リトライ" (attempt 1 -> 2) is immediate.
	if gap := timestamps[1].Sub(timestamps[0]); gap >= baseDelay {
		t.Errorf("gap between attempt 1 and 2 = %v, want well under the backoff base delay %v (immediate retry)", gap, baseDelay)
	}
	// "2回目以降exponential backoff" (attempt 2 -> 3) waits >= baseDelay.
	if gap := timestamps[2].Sub(timestamps[1]); gap < baseDelay {
		t.Errorf("gap between attempt 2 and 3 = %v, want at least the backoff base delay %v", gap, baseDelay)
	}
}

func TestClient_Scout_ContextCancellationDuringBackoffAbortsPromptly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 5, RetryBaseDelay: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := client.Scout(ctx, jev.ScoutRequest{})
	if err == nil {
		t.Fatal("Scout: want error from context cancellation, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Scout took %v to return after context cancellation, want it to abort promptly instead of waiting out the 5s backoff", elapsed)
	}
}

func TestClient_Scout_NonOKStatusReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	if err == nil {
		t.Fatal("Scout: want error for a 401 response, got nil")
	}
}

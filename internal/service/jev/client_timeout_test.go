package jev_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// non-functional.md §2.2: a Jev call times out after 5s (per attempt), not
// the former 10s. A server that never answers must fail the default client
// at ~5s.
func TestClient_Scout_DefaultTimeoutIsFiveSeconds(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	start := time.Now()
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{QuestionVersion: "scout-v1"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Scout() error = nil, want timeout error against a hanging server")
	}
	if elapsed < 4500*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("Scout() timed out after %v, want ~5s", elapsed)
	}
}

// TestClient_Healthy_FollowsRollingErrorRate: Healthy (the
// jev_api_down HealthChecker) is false exactly while the rolling error
// rate is at or above the threshold, and recovers once enough successful
// calls slide the failures out of the window.
func TestClient_Healthy_FollowsRollingErrorRate(t *testing.T) {
	// Window 6, threshold 50%: [T,T,T,T,F] breaches at call 5; by call 9
	// the window is [T,F,F,F,F,F] (1/6) and is healthy again.
	fails := []bool{true, true, true, true, false, false, false, false, false}
	server := scriptedServer(t, fails)
	defer server.Close()

	client := jev.NewClient(jev.Config{
		BaseURL:            server.URL,
		MaxAttempts:        1,
		ErrorRateWindow:    6,
		ErrorRateThreshold: 0.5,
	})
	ctx := context.Background()
	healthy := func() bool {
		t.Helper()
		ok, err := client.Healthy(ctx)
		if err != nil {
			t.Fatalf("Healthy: %v", err)
		}
		return ok
	}

	if !healthy() {
		t.Fatal("Healthy() = false before any call, want true")
	}
	for i := range fails {
		_, _, _ = client.Scout(ctx, jev.ScoutRequest{QuestionVersion: "v1"})
		switch calls := i + 1; {
		case calls < 5 && !healthy():
			t.Fatalf("Healthy() = false after %d calls, want true (below the minimum sample count)", calls)
		case calls == 5 && healthy():
			t.Fatal("Healthy() = true after call 5 ([T,T,T,T,F], 80% failed), want false")
		}
	}
	if !healthy() {
		t.Fatal("Healthy() = false after the window recovered, want true")
	}
}

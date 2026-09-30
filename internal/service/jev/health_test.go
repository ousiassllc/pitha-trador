package jev_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

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

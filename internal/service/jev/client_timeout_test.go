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

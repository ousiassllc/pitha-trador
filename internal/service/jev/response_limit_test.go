package jev_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

func TestClient_Scout_RejectsOversizedResponseWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(strings.Repeat("x", httpbody.DefaultMaxBytes+1)))
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 3})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{State: jev.ScoutState{Symbol: "1301"}})
	if !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("Scout err = %v, want httpbody.ErrTooLarge", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (an oversized response must not be retried)", got)
	}
}

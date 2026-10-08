package marketdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// boardServer answers /token and /board/<symbol>@1 with status (200 = a
// valid board, anything else = an error response).
func boardServer(t *testing.T, status *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
			return
		}
		if *status != http.StatusOK {
			w.WriteHeader(*status)
			_, _ = w.Write([]byte(`{"Code":4001999,"Message":"boom"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": "7203", "CurrentPrice": 2409.0})
	}))
}

// TestClient_Healthy_MarketDataDownAfterConsecutiveBoardFailures covers
// FR-RISK-2's 市場データ停止: 5 consecutive GetBoard failures flip
// Healthy to false and the first success flips it back (FR-RISK-7).
func TestClient_Healthy_MarketDataDownAfterConsecutiveBoardFailures(t *testing.T) {
	status := http.StatusBadGateway
	server := boardServer(t, &status)
	defer server.Close()
	ctx := context.Background()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	healthy := func() bool {
		t.Helper()
		ok, err := broker.MarketDataChecker{Health: client}.Healthy(ctx)
		if err != nil {
			t.Fatalf("Healthy: %v", err)
		}
		return ok
	}
	for i := 1; i <= 5; i++ {
		if !healthy() {
			t.Fatalf("Healthy() = false after %d failures, want true", i-1)
		}
		if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err == nil {
			t.Fatal("GetBoard unexpectedly succeeded")
		}
	}
	if healthy() {
		t.Fatal("Healthy() = true after 5 consecutive failures, want false")
	}

	status = http.StatusOK
	if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if !healthy() {
		t.Fatal("Healthy() = false after a successful GetBoard, want true")
	}
}

// TestClient_BrokerFailures_CountsConsecutive5xxOnly: only HTTP 5xx
// responses extend the broker_api_error streak; a 4xx is a request
// problem and a success ends the streak.
func TestClient_BrokerFailures_CountsConsecutive5xxOnly(t *testing.T) {
	status := http.StatusInternalServerError
	server := boardServer(t, &status)
	defer server.Close()
	ctx := context.Background()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	streak := client.BrokerFailures()
	board := func() { _, _ = client.GetBoard(ctx, "7203", marketdata.ExchangeTSE) }

	board()
	board()
	if got := streak.ConsecutiveFailures(); got != 2 {
		t.Fatalf("streak after two 500s = %d, want 2", got)
	}

	status = http.StatusBadRequest
	board()
	if got := streak.ConsecutiveFailures(); got != 2 {
		t.Fatalf("streak after a 400 = %d, want 2 (unchanged)", got)
	}

	status = http.StatusOK
	board()
	if got := streak.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after a success = %d, want 0", got)
	}
}

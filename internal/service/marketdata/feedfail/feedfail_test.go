// Package feedfail holds the external test of which GetBoard failures
// extend the market_data_down streak (issue #532). It lives in its own
// directory to keep internal/service/marketdata under the per-directory
// line limit.
package feedfail_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// TestClient_Healthy_BoardFailureClassification: only feed-level GetBoard
// failures (5xx, transport error, no token, auth/token 4xx) extend the
// market_data_down streak; a per-symbol 4xx such as 4002001 must not, so
// invalid symbols cannot trip the Kill Switch while the feed is fine.
// Every failure still marks the symbol stale.
func TestClient_Healthy_BoardFailureClassification(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		code        int
		withToken   bool
		closeServer bool
		wantHealthy bool
	}{
		{name: "4002001 symbol not found", status: http.StatusBadRequest, code: 4002001, withToken: true, wantHealthy: true},
		{name: "other 4xx", status: http.StatusBadRequest, code: 4001999, withToken: true, wantHealthy: true},
		{name: "404 without code", status: http.StatusNotFound, withToken: true, wantHealthy: true},
		{name: "500", status: http.StatusInternalServerError, code: 4001001, withToken: true, wantHealthy: false},
		{name: "502", status: http.StatusBadGateway, withToken: true, wantHealthy: false},
		{name: "4001009 api key mismatch", status: http.StatusBadRequest, code: 4001009, withToken: true, wantHealthy: false},
		{name: "4001007 not logged in", status: http.StatusBadRequest, code: 4001007, withToken: true, wantHealthy: false},
		{name: "401", status: http.StatusUnauthorized, withToken: true, wantHealthy: false},
		{name: "no token", withToken: false, wantHealthy: false},
		{name: "connection error", withToken: true, closeServer: true, wantHealthy: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
					return
				}
				w.WriteHeader(tt.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"Code": tt.code, "Message": "x"})
			}))
			defer server.Close()
			ctx := context.Background()

			client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
			if tt.withToken {
				if _, err := client.IssueToken(ctx); err != nil {
					t.Fatalf("IssueToken: %v", err)
				}
			}
			if tt.closeServer {
				server.Close()
			}
			for i := 0; i < 5; i++ {
				if _, err := client.GetBoard(ctx, "7203", marketdata.ExchangeTSE); err == nil {
					t.Fatal("GetBoard unexpectedly succeeded")
				}
			}
			healthy, err := client.Healthy(ctx)
			if err != nil {
				t.Fatalf("Healthy: %v", err)
			}
			if healthy != tt.wantHealthy {
				t.Errorf("Healthy() = %v after 5 failures, want %v", healthy, tt.wantHealthy)
			}
			if !client.Status().IsStale("7203") {
				t.Error("symbol not marked stale after failed GetBoard")
			}
		})
	}
}

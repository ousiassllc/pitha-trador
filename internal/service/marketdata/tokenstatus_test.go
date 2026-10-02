package marketdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestClient_TokenStatus_ClassifiesFailure(t *testing.T) {
	tests := []struct {
		name      string
		code      int
		wantIssue marketdata.TokenIssue
		wantHint  string
	}{
		{name: "4001013 wrong API password", code: 4001013, wantIssue: marketdata.TokenIssueBadPassword, wantHint: "KABU_API_PASSWORD"},
		{name: "4001007 not logged in", code: 4001007, wantIssue: marketdata.TokenIssueNotLoggedIn, wantHint: "ログイン"},
		{name: "4001017 not logged in", code: 4001017, wantIssue: marketdata.TokenIssueNotLoggedIn, wantHint: "ログイン"},
		{name: "4001008 API disabled", code: 4001008, wantIssue: marketdata.TokenIssueAPIDisabled, wantHint: "APIを利用する"},
		{name: "other code", code: 4001999, wantIssue: marketdata.TokenIssueUnknown, wantHint: "4001999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"Code": tt.code, "Message": "x"})
			}))
			defer server.Close()

			client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
			if got := client.TokenStatus(); got.Failed() {
				t.Fatalf("TokenStatus before any issuance = %+v, want no failure", got)
			}
			if _, err := client.IssueToken(context.Background()); err == nil {
				t.Fatal("IssueToken: want error, got nil")
			}
			got := client.TokenStatus()
			if got.Issue != tt.wantIssue || got.Code != tt.code {
				t.Errorf("TokenStatus = %+v, want issue %q code %d", got, tt.wantIssue, tt.code)
			}
			if !strings.Contains(got.Guidance(), tt.wantHint) {
				t.Errorf("Guidance = %q, want it to mention %q", got.Guidance(), tt.wantHint)
			}
		})
	}
}

func TestClient_TokenStatus_ConnectionRefusedIsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // nothing listens on url any more: connection refused

	client := marketdata.NewClient(marketdata.Config{BaseURL: url, APIPassword: "pw"})
	if _, err := client.IssueToken(context.Background()); err == nil {
		t.Fatal("IssueToken: want error, got nil")
	}
	got := client.TokenStatus()
	if got.Issue != marketdata.TokenIssueUnreachable {
		t.Errorf("TokenStatus = %+v, want issue %q", got, marketdata.TokenIssueUnreachable)
	}
	if !strings.Contains(got.Guidance(), "起動") {
		t.Errorf("Guidance = %q, want it to tell the operator to start kabuステーション", got.Guidance())
	}
}

func TestClient_TokenStatus_ClearsOnSuccess(t *testing.T) {
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001013, "Message": "x"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
	_, _ = client.IssueToken(context.Background())
	if !client.TokenStatus().Failed() {
		t.Fatal("TokenStatus.Failed = false after a failed issuance")
	}
	healthy.Store(true)
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if got := client.TokenStatus(); got.Failed() || got.Guidance() != "" {
		t.Errorf("TokenStatus after success = %+v, want cleared", got)
	}
}

// A failed initial issuance must not leave the app tokenless forever
// (issue #295): once kabuステーション comes up, the background retry obtains
// the token without a restart.
func TestClient_Start_RetriesAfterInitialFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001007, "Message": "x"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-late"})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx, 10*time.Millisecond); err == nil {
		t.Fatal("Start: want the initial issuance error, got nil")
	}
	if got := client.TokenStatus().Issue; got != marketdata.TokenIssueNotLoggedIn {
		t.Errorf("TokenStatus.Issue right after failed Start = %q, want %q", got, marketdata.TokenIssueNotLoggedIn)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tok, ok := client.Token(); ok {
			if tok != "tok-late" || client.TokenStatus().Failed() {
				t.Fatalf("recovered token = %q, status = %+v", tok, client.TokenStatus())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no token obtained by background retry after the initial failure")
}

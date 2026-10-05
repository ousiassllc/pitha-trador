package marketdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestClient_IssueToken(t *testing.T) {
	var gotPassword string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			APIPassword string `json:"APIPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotPassword = body.APIPassword
		_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-123"})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	token, err := client.IssueToken(context.Background())
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if token != "tok-123" {
		t.Errorf("token = %q, want %q", token, "tok-123")
	}
	if gotPassword != "secret" {
		t.Errorf("APIPassword sent = %q, want %q", gotPassword, "secret")
	}

	held, ok := client.Token()
	if !ok || held != "tok-123" {
		t.Errorf("Token() = (%q, %v), want (%q, true)", held, ok, "tok-123")
	}
}

func TestClient_IssueToken_ResultCodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 4001001, "Token": ""})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "wrong"})
	if _, err := client.IssueToken(context.Background()); err == nil {
		t.Fatal("IssueToken: want error for non-zero ResultCode, got nil")
	}
	if _, ok := client.Token(); ok {
		t.Error("Token() reports a token was held despite the failed issuance")
	}
}

// TestNewClient_EmptyConfigDoesNotPanic proves internal/bootstrap.
// BuildServices can safely construct a Client even when
// config.Secrets.KabuAPIPassword is empty (issue #57: the Settings
// screen lets an operator leave it unset until the app is restarted, so
// BuildServices must not panic on a zero-value marketdata.Config - it
// should simply fail every kabuステーションAPI call at runtime instead).
func TestNewClient_EmptyConfigDoesNotPanic(t *testing.T) {
	client := marketdata.NewClient(marketdata.Config{})
	if client == nil {
		t.Fatal("NewClient(Config{}) returned nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := client.IssueToken(ctx); err == nil {
		t.Fatal("IssueToken: expected an error with no kabuステーションAPI listening at DefaultBaseURL, got nil")
	}
}

func TestClient_Start_ReissuesTokenPeriodically(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx, 15*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	cancel()

	if got := count.Load(); got < 3 {
		t.Errorf("token issuance count = %d, want at least 3 within 80ms at a 15ms interval", got)
	}
}

func TestClient_Start_KeepsPreviousTokenOnReissueFailure(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-initial"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"Code": 500, "Message": "down"})
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx, 10*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	token, ok := client.Token()
	if !ok || token != "tok-initial" {
		t.Errorf("Token() = (%q, %v), want (%q, true) after failed reissues", token, ok, "tok-initial")
	}
}

func TestClient_IssueToken_RejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", httpbody.DefaultMaxBytes+1)))
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	_, err := client.IssueToken(context.Background())
	if !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("IssueToken err = %v, want httpbody.ErrTooLarge", err)
	}
}

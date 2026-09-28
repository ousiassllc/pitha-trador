package marketdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestClient_RegisterSymbols_RequiresToken(t *testing.T) {
	client := marketdata.NewClient(marketdata.Config{BaseURL: "http://unused.invalid"})
	if _, err := client.RegisterSymbols(context.Background(), nil); err != marketdata.ErrNoToken {
		t.Errorf("RegisterSymbols before token issuance: err = %v, want %v", err, marketdata.ErrNoToken)
	}
}

func TestClient_RegisterSymbols(t *testing.T) {
	var gotToken string
	var gotSymbols []marketdata.RegisterSymbol
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-abc"})
		case "/register":
			if r.Method != http.MethodPut {
				t.Fatalf("register method = %s, want PUT", r.Method)
			}
			gotToken = r.Header.Get("X-API-KEY")
			var body struct {
				Symbols []marketdata.RegisterSymbol `json:"Symbols"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotSymbols = body.Symbols
			_ = json.NewEncoder(w).Encode(map[string]any{"RegistList": body.Symbols})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	want := []marketdata.RegisterSymbol{{Symbol: "7203", Exchange: marketdata.ExchangeTSE}}
	resp, err := client.RegisterSymbols(context.Background(), want)
	if err != nil {
		t.Fatalf("RegisterSymbols: %v", err)
	}
	if gotToken != "tok-abc" {
		t.Errorf("X-API-KEY sent = %q, want %q", gotToken, "tok-abc")
	}
	if len(gotSymbols) != 1 || gotSymbols[0].Symbol != "7203" {
		t.Errorf("symbols sent = %+v, want %+v", gotSymbols, want)
	}
	if len(resp.RegistList) != 1 || resp.RegistList[0].Symbol != "7203" {
		t.Errorf("RegisterSuccess.RegistList = %+v", resp.RegistList)
	}
}

func TestClient_GetBoard_MarksFreshOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok-abc"})
		case "/board/7203@1":
			bid := 2408.5
			ask := 2409.0
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Symbol":        "7203",
				"CurrentPrice":  2409.0,
				"VWAP":          2394.4262,
				"TradingVolume": 1234567.0,
				"TradingValue":  10946119350.0,
				"BidPrice":      bid,
				"AskPrice":      ask,
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	board, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if board.CurrentPrice != 2409.0 || board.VWAP != 2394.4262 {
		t.Errorf("board = %+v", board)
	}
	if board.BidPrice == nil || *board.BidPrice != 2408.5 {
		t.Errorf("board.BidPrice = %v, want 2408.5", board.BidPrice)
	}

	if client.Status().IsStale("7203") {
		t.Error("symbol marked stale after a successful GetBoard")
	}
}

func TestClient_GetBoard_NullBidAsk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/board/1301@1":
			// kabuステーションAPI returns null bid/ask before the symbol
			// has been registered (BoardSuccess description); FR-FE-2
			// requires this be treated as a missing value, not zero.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Symbol":       "1301",
				"CurrentPrice": 3000.0,
				"BidPrice":     nil,
				"AskPrice":     nil,
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	board, err := client.GetBoard(context.Background(), "1301", marketdata.ExchangeTSE)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if board.BidPrice != nil || board.AskPrice != nil {
		t.Errorf("board bid/ask = (%v, %v), want (nil, nil)", board.BidPrice, board.AskPrice)
	}
}

func TestClient_GetBoard_MarksStaleOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/board/9999@1":
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4001001, "Message": "内部エラー"})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	if _, err := client.GetBoard(context.Background(), "9999", marketdata.ExchangeTSE); err == nil {
		t.Fatal("GetBoard: want error, got nil")
	}
	if !client.Status().IsStale("9999") {
		t.Error("symbol not marked stale after a failed GetBoard")
	}

	status, ok := client.Status().Status("9999")
	if !ok || status.LastError == nil {
		t.Errorf("Status(9999) = (%+v, %v), want an error recorded", status, ok)
	}
}

func TestClient_GetBoard_RequiresToken(t *testing.T) {
	client := marketdata.NewClient(marketdata.Config{BaseURL: "http://unused.invalid"})
	if _, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE); err != marketdata.ErrNoToken {
		t.Errorf("GetBoard before token issuance: err = %v, want %v", err, marketdata.ErrNoToken)
	}
}

func TestClient_Start_ReissuesTokenPeriodically(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
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

	if count < 3 {
		t.Errorf("token issuance count = %d, want at least 3 within 80ms at a 15ms interval", count)
	}
}

func TestClient_Start_KeepsPreviousTokenOnReissueFailure(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
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

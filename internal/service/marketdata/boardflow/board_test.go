package boardflow_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestClient_RegisterSymbols_RequiresToken(t *testing.T) {
	client := marketdata.NewClient(marketdata.Config{BaseURL: "http://unused.invalid"})
	if _, err := client.RegisterSymbols(context.Background(), nil); err != broker.ErrNoSession {
		t.Errorf("RegisterSymbols before token issuance: err = %v, want %v", err, broker.ErrNoSession)
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
			// kabu naming is trader-side: BidPrice is the best SELL quote
			// and AskPrice the best BUY quote, so BidPrice > AskPrice
			// (kabu_STATION_API.yaml BoardSuccess sample).
			bid := 2408.5
			ask := 2407.5
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
	if board.AskPrice == nil || *board.AskPrice != 2407.5 {
		t.Errorf("board.AskPrice = %v, want 2407.5", board.AskPrice)
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
	if _, err := client.GetBoard(context.Background(), "7203", marketdata.ExchangeTSE); err != broker.ErrNoSession {
		t.Errorf("GetBoard before token issuance: err = %v, want %v", err, broker.ErrNoSession)
	}
}

func TestBoard_DepthSumsReportedLevels(t *testing.T) {
	b := marketdata.Board{
		Sell1: &marketdata.BoardLevel{Price: 2101, Qty: 300}, Sell3: &marketdata.BoardLevel{Price: 2103, Qty: 200},
		Buy1: &marketdata.BoardLevel{Price: 2099, Qty: 100},
	}
	if d, ok := b.SellDepth(); !ok || d != 500 {
		t.Errorf("SellDepth = %v, %v; want 500, true", d, ok)
	}
	if d, ok := b.BuyDepth(); !ok || d != 100 {
		t.Errorf("BuyDepth = %v, %v; want 100, true", d, ok)
	}
	if _, ok := (marketdata.Board{}).SellDepth(); ok {
		t.Error("SellDepth ok = true with no levels, want false (FR-FE-2)")
	}
}

func TestBoard_HasPrice(t *testing.T) {
	for name, tc := range map[string]struct {
		price float64
		want  bool
	}{"positive": {2500, true}, "zero": {0, false}, "negative": {-1, false}, "nan": {math.NaN(), false}, "inf": {math.Inf(1), false}} {
		if got := (marketdata.Board{CurrentPrice: tc.price}).HasPrice(); got != tc.want {
			t.Errorf("%s: HasPrice() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestBoardCache_FreshnessAndMissingPrice(t *testing.T) {
	cache := marketdata.NewBoardCache()
	at := time.Date(2026, 9, 29, 9, 30, 0, 0, time.UTC)

	cache.Put(marketdata.Board{Symbol: "7203", CurrentPrice: 2500}, at)
	if b, ok := cache.Fresh("7203", at.Add(10*time.Second), 30*time.Second); !ok || b.CurrentPrice != 2500 {
		t.Fatalf("Fresh within maxAge = (%+v, %v), want the cached board", b, ok)
	}
	if _, ok := cache.Fresh("7203", at.Add(31*time.Second), 30*time.Second); ok {
		t.Error("Fresh beyond maxAge = true, want false")
	}
	if _, ok := cache.Fresh("6758", at, time.Minute); ok {
		t.Error("Fresh for unseen symbol = true, want false")
	}

	// A price-less PUSH message must not replace the last good board.
	cache.Put(marketdata.Board{Symbol: "7203", CurrentPrice: 0}, at.Add(time.Second))
	if b, ok := cache.Fresh("7203", at.Add(2*time.Second), 30*time.Second); !ok || b.CurrentPrice != 2500 {
		t.Errorf("after price-0 Put, Fresh = (%+v, %v), want the earlier 2500 board", b, ok)
	}
}

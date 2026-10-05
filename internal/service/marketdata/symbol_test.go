package marketdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func fp(v float64) *float64 { return &v }

func TestSymbolInfo_PriceLimit(t *testing.T) {
	info := marketdata.SymbolInfo{UpperLimit: fp(2500), LowerLimit: fp(2000)}
	cases := []struct {
		price float64
		want  domain.PriceLimit
	}{
		{2500, domain.PriceLimitUp},
		{2000, domain.PriceLimitDown},
		{2499, domain.PriceLimitNone},
		{2001, domain.PriceLimitNone},
	}
	for _, c := range cases {
		if got := info.PriceLimit(c.price); got != c.want {
			t.Errorf("PriceLimit(%v) = %q, want %q", c.price, got, c.want)
		}
	}
	if got := (marketdata.SymbolInfo{}).PriceLimit(1); got != domain.PriceLimitNone {
		t.Errorf("unknown limits: PriceLimit = %q, want none", got)
	}
}

func TestBoard_IsSpecialQuote(t *testing.T) {
	cases := []struct {
		bid, ask string
		want     bool
	}{
		{"", "", false},
		{"0101", "0101", false}, // 一般気配
		{"0102", "", true},      // 特別気配
		{"0101", "0102", true},
		{"0108", "", true}, // 停止前特別気配
	}
	for _, c := range cases {
		if got := (marketdata.Board{BidSign: c.bid, AskSign: c.ask}).IsSpecialQuote(); got != c.want {
			t.Errorf("IsSpecialQuote(%q, %q) = %v, want %v", c.bid, c.ask, got, c.want)
		}
	}
}

func TestClient_GetSymbol_DecodesLendabilityAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/symbol/7203@1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Symbol": "7203", "MarginSell": false, "UpperLimit": 2500.0, "LowerLimit": 2000.0,
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.GetSymbol(context.Background(), "7203", marketdata.ExchangeTSE); !errors.Is(err, marketdata.ErrNoToken) {
		t.Fatalf("before token: err = %v, want ErrNoToken", err)
	}
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	info, err := client.GetSymbol(context.Background(), "7203", marketdata.ExchangeTSE)
	if err != nil {
		t.Fatalf("GetSymbol: %v", err)
	}
	if info.MarginSell == nil || *info.MarginSell || *info.UpperLimit != 2500 || *info.LowerLimit != 2000 {
		t.Errorf("info = %+v", info)
	}
}

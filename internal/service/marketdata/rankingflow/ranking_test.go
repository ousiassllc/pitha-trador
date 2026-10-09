package rankingflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func rankingClient(t *testing.T, ranking http.HandlerFunc) *marketdata.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
			return
		}
		if r.URL.Path != "/ranking" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		ranking(w, r)
	}))
	t.Cleanup(server.Close)
	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "secret"})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return client
}

func TestClient_MeasureRanking_ReducesResponseToMeasurements(t *testing.T) {
	client := rankingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("Type"); got != "3" {
			t.Errorf("Type = %q, want 3", got)
		}
		if got := r.URL.Query().Get("ExchangeDivision"); got != "TP" {
			t.Errorf("ExchangeDivision = %q, want TP", got)
		}
		if got := r.Header.Get("X-API-KEY"); got != "tok" {
			t.Errorf("X-API-KEY = %q, want tok", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Type": "3", "ExchangeDivision": "TP",
			"Ranking": []map[string]any{
				{"No": 1, "Symbol": "7203", "CurrentPrice": 2500.0, "CurrentPriceTime": "09:15"},
				{"No": 2, "Symbol": "9433", "CurrentPrice": 3000.0, "CurrentPriceTime": "09:16"},
				{"No": 2, "Symbol": "6758", "CurrentPrice": 1500.0, "CurrentPriceTime": "09:14"},
				{"No": 4, "Symbol": "1301", "CurrentPrice": 900.0, "CurrentPriceTime": "09:09"},
			},
		})
	})
	got, err := client.MeasureRanking(context.Background(), 3, "TP")
	if err != nil {
		t.Fatalf("MeasureRanking: %v", err)
	}
	want := marketdata.RankingMeasurement{Count: 4, DuplicateRanks: 1, LatestPriceTime: "09:16"}
	if got != want {
		t.Errorf("MeasureRanking = %+v, want %+v", got, want)
	}
}

func TestClient_MeasureRanking_EmptyRankingIsZeroMeasurement(t *testing.T) {
	client := rankingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`)) // 平日7:53頃〜9:00過ぎは空で返る
	})
	got, err := client.MeasureRanking(context.Background(), 1, "T")
	if err != nil {
		t.Fatalf("MeasureRanking: %v", err)
	}
	if got != (marketdata.RankingMeasurement{}) {
		t.Errorf("MeasureRanking = %+v, want zero", got)
	}
}

func TestClient_MeasureRanking_ReturnsAPIError(t *testing.T) {
	client := rankingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"Code": 100001, "Message": "bad"})
	})
	_, err := client.MeasureRanking(context.Background(), 1, "T")
	var api *marketdata.APIError
	if !errors.As(err, &api) || api.StatusCode != http.StatusBadRequest || api.Code != 100001 {
		t.Fatalf("err = %v, want *APIError{400, 100001}", err)
	}
}

func TestClient_MeasureRanking_NoToken(t *testing.T) {
	client := marketdata.NewClient(marketdata.Config{BaseURL: "http://127.0.0.1:1", APIPassword: "secret"})
	if _, err := client.MeasureRanking(context.Background(), 1, "T"); !errors.Is(err, broker.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestClient_RankingSymbols_KeepsOnlyCodesInRankOrderWithoutDuplicates(t *testing.T) {
	client := rankingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("Type"); got != "6" {
			t.Errorf("Type = %q, want 6", got)
		}
		if got := r.URL.Query().Get("ExchangeDivision"); got != "T" {
			t.Errorf("ExchangeDivision = %q, want T", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Ranking": []map[string]any{
				{"No": 1, "Symbol": "7203", "CurrentPrice": 2500.0},
				{"No": 2, "Symbol": "9433", "CurrentPrice": 3000.0},
				{"No": 2, "Symbol": "7203", "CurrentPrice": 2500.0}, // 同順位の重複
				{"No": 4, "Symbol": "", "CurrentPrice": 900.0},
				{"No": 5, "Symbol": "6758", "CurrentPrice": 1500.0},
			},
		})
	})
	got, err := client.RankingSymbols(context.Background(), 6, "T")
	if err != nil {
		t.Fatalf("RankingSymbols: %v", err)
	}
	if want := []string{"7203", "9433", "6758"}; !slices.Equal(got, want) {
		t.Errorf("RankingSymbols = %v, want %v", got, want)
	}
}

func TestClient_RankingSymbols_EmptyRankingIsNotAnError(t *testing.T) {
	client := rankingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`)) // 平日7:53頃〜9:00過ぎは空で返る
	})
	got, err := client.RankingSymbols(context.Background(), 1, "T")
	if err != nil || len(got) != 0 {
		t.Fatalf("RankingSymbols = (%v, %v), want empty and no error", got, err)
	}
}

func TestClient_RankingSymbols_FailuresAreReturned(t *testing.T) {
	apiErr := rankingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"Code": 100001, "Message": "bad"})
	})
	var api *marketdata.APIError
	if _, err := apiErr.RankingSymbols(context.Background(), 1, "T"); !errors.As(err, &api) || api.Code != 100001 {
		t.Fatalf("err = %v, want *APIError{100001}", err)
	}
	broken := rankingClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not json`)) })
	if _, err := broken.RankingSymbols(context.Background(), 1, "T"); err == nil {
		t.Fatal("decode error was swallowed")
	}
	noToken := marketdata.NewClient(marketdata.Config{BaseURL: "http://127.0.0.1:1", APIPassword: "secret"})
	if _, err := noToken.RankingSymbols(context.Background(), 1, "T"); !errors.Is(err, broker.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

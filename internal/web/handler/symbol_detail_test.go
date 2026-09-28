package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestSymbolHandler_APISymbol_ReturnsStateAndRiskParams(t *testing.T) {
	confidence := 0.74
	provider := &fakeSymbolProvider{state: execution.SymbolState{
		Symbol: "7203", LastPrice: 2831.5, LastSignal: domain.JevDirectionLong, LastSignalConfidence: confidence,
		Position: &domain.Position{Side: domain.PositionSideLong, Quantity: 100},
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{AllowedPositionPct: 2.0, StopLossPct: 0.6, TakeProfitPct: 1.2})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}", h.APISymbol)

	resp := api.Get("/symbols/7203")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body struct {
		Symbol          string  `json:"symbol"`
		Price           float64 `json:"price"`
		CurrentPosition float64 `json:"current_position"`
		Risk            struct {
			AllowedPositionPct float64 `json:"allowed_position_pct"`
			StopLossPct        float64 `json:"stop_loss_pct"`
			TakeProfitPct      float64 `json:"take_profit_pct"`
		} `json:"risk"`
		Jev struct {
			Direction  string  `json:"direction"`
			Confidence float64 `json:"confidence"`
		} `json:"jev"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}
	if body.Symbol != "7203" || body.Price != 2831.5 {
		t.Fatalf("body = %+v, want Symbol=7203 Price=2831.5", body)
	}
	if body.CurrentPosition != 100 {
		t.Fatalf("current_position = %v, want 100 (LONG size)", body.CurrentPosition)
	}
	if body.Risk.AllowedPositionPct != 2.0 || body.Risk.StopLossPct != 0.6 || body.Risk.TakeProfitPct != 1.2 {
		t.Fatalf("risk = %+v, want the configured SymbolRiskParams", body.Risk)
	}
	if body.Jev.Direction != domain.JevDirectionLong || body.Jev.Confidence != confidence {
		t.Fatalf("jev = %+v, want Direction=LONG Confidence=%v", body.Jev, confidence)
	}
}

func TestSymbolHandler_APISymbol_ShortPositionReportsNegativeSize(t *testing.T) {
	provider := &fakeSymbolProvider{state: execution.SymbolState{
		Symbol: "7203", LastSignal: domain.JevDirectionNone,
		Position: &domain.Position{Side: domain.PositionSideShort, Quantity: 50},
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}", h.APISymbol)

	resp := api.Get("/symbols/7203")

	var body struct {
		CurrentPosition float64 `json:"current_position"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body.CurrentPosition != -50 {
		t.Fatalf("current_position = %v, want -50 (SHORT size)", body.CurrentPosition)
	}
}

func TestSymbolHandler_APISymbol_UnknownSymbolReturns404(t *testing.T) {
	provider := &fakeSymbolProvider{stateErr: execution.ErrInstrumentUnknown}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}", h.APISymbol)

	resp := api.Get("/symbols/9999")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

func TestSymbolHandler_APICandles_DefaultsToSixHourLookback(t *testing.T) {
	provider := &fakeSymbolProvider{candles: []domain.Snapshot{
		{Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2105.0, Volume: 1000, Feature: domain.Feature{VWAP: 2100.0}},
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}/candles", h.APICandles)

	resp := api.Get("/symbols/7203/candles")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	if got := provider.lastCandlesTo.Sub(provider.lastCandlesFrom); got != 6*time.Hour {
		t.Fatalf("resolved lookback window = %v, want 6h", got)
	}

	var body struct {
		Symbol  string `json:"symbol"`
		Candles []struct {
			Open   float64 `json:"open"`
			High   float64 `json:"high"`
			Low    float64 `json:"low"`
			Close  float64 `json:"close"`
			Volume int64   `json:"volume"`
			VWAP   float64 `json:"vwap"`
		} `json:"candles"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}
	if len(body.Candles) != 1 {
		t.Fatalf("candles = %+v, want exactly 1", body.Candles)
	}
	c := body.Candles[0]
	if c.Open != 2105.0 || c.High != 2105.0 || c.Low != 2105.0 || c.Close != 2105.0 || c.VWAP != 2100.0 || c.Volume != 1000 {
		t.Fatalf("candle = %+v, want Open=High=Low=Close=2105.0 VWAP=2100.0 Volume=1000", c)
	}
}

func TestSymbolHandler_APICandles_RespectsExplicitFromTo(t *testing.T) {
	provider := &fakeSymbolProvider{}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}/candles", h.APICandles)

	resp := api.Get("/symbols/7203/candles?from=2026-09-27T00:00:00Z&to=2026-09-27T01:00:00Z")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	wantFrom := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	if !provider.lastCandlesFrom.Equal(wantFrom) || !provider.lastCandlesTo.Equal(wantTo) {
		t.Fatalf("resolved from/to = %v/%v, want %v/%v", provider.lastCandlesFrom, provider.lastCandlesTo, wantFrom, wantTo)
	}
}

func TestSymbolHandler_APICandles_InvalidFromReturns400(t *testing.T) {
	provider := &fakeSymbolProvider{}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}/candles", h.APICandles)

	resp := api.Get("/symbols/7203/candles?from=not-a-date")

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

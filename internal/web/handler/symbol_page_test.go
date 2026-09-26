package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestSymbolHandler_Page_RendersDetailPanelsAndPriceChartIsland(t *testing.T) {
	gin.SetMode(gin.TestMode)
	direction := domain.JevDirectionLong
	confidence := 0.74
	regime := domain.JevRegimeBreakout
	entryQuality := domain.JevEntryQualityStrong
	provider := &fakeSymbolProvider{
		state: execution.SymbolState{Symbol: "7203", LastPrice: 2831.5, LastSignal: domain.JevDirectionLong},
		decisions: []domain.JevDecision{{
			ID: 1, DecisionType: domain.JevDecisionTypeTrader,
			Direction: &direction, Confidence: &confidence, Regime: &regime, EntryQuality: &entryQuality,
		}},
	}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{AllowedPositionPct: 2.0, StopLossPct: 0.6, TakeProfitPct: 1.2})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/symbols/7203", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<h1>7203</h1>",
		`<pitha-price-chart`,
		`candles-url="/api/v1/symbols/7203/candles"`,
		`ws-url="/ws/symbols/7203"`,
		`id="jev-panel"`,
		"BREAKOUT",
		`id="risk-panel"`,
		"0.60",
		`id="decision-history"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q; body=%s", want, body)
		}
	}
}

func TestSymbolHandler_Page_UnknownSymbolReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{stateErr: execution.ErrInstrumentUnknown}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/symbols/9999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSymbolHandler_Page_NoOpenPositionShowsPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionNone}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/symbols/7203", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No open position.") {
		t.Fatalf("body = %s, want the no-open-position placeholder", rec.Body.String())
	}
}

package symbol_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
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
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{AllowedPositionPct: 2.0, StopLossPct: 0.6, TakeProfitPct: 1.2})
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
		">7203</h1>",
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
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/symbols/9999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
		t.Fatalf("404 should render the error page, got body %q", rec.Body.String())
	}
}

// Only ErrInstrumentUnknown is a 404; any other failure is a 500 whose page
// shows a fixed message, never err.Error() (issue #143).
func TestSymbolHandler_Page_OtherStateErrorReturns500ErrorPageWithoutLeakingDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{stateErr: errors.New("sqlite: database is locked /var/db/pitha.db")}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/symbols/7203", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="error-page"`) {
		t.Fatalf("500 should render the error page, got body %q", body)
	}
	if strings.Contains(body, "database is locked") || strings.Contains(body, "/var/db") {
		t.Fatalf("error page leaks the internal error: %s", body)
	}
}

// An HTMX request gets the toast fragment, not a nested full page.
func TestSymbolHandler_Page_HXRequestErrorGetsToastFragment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := symbol.NewSymbolHandler(&fakeSymbolProvider{stateErr: execution.ErrInstrumentUnknown}, symbol.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/symbols/9999", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || rec.Body.Len() == 0 || strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("HX 404 = %d body %q, want toast fragment", rec.Code, rec.Body.String())
	}
}

func TestSymbolHandler_Page_NoOpenPositionShowsPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionNone}}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
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

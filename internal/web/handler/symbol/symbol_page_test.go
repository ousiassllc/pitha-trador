package symbol_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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

// Issue #322: the page used to stream into c.Writer and drop the Render
// error, so a failing template left a truncated 200 and no log. A render
// failure (here: a request context that is already cancelled, which templ
// components report as an error) must be logged and answered with a 500.
func TestSymbolHandler_Page_RenderFailureIs500AndLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	provider := &fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionNone}}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/symbols/7203", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `id="jev-panel"`) {
		t.Errorf("partial page leaked into the response: %q", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "handler: render") || !strings.Contains(logs.String(), "context canceled") {
		t.Errorf("render error not logged: %q", logs.String())
	}
}

// issue #382: Page validates the symbol format (same as the JSON API's
// SymbolPathInput) before consulting the provider. The fake provider
// resolves every symbol, so a 404 here can only come from the format check.
func TestSymbolHandler_Page_InvalidSymbolFormatReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{state: execution.SymbolState{Symbol: "x", LastPrice: 1}}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	router := gin.New()
	router.GET("/symbols/:symbol", h.Page)

	for name, path := range map[string]string{
		"dot":       "/symbols/a.b",
		"question":  "/symbols/a%3Fb",
		"hash":      "/symbols/a%23b",
		"percent":   "/symbols/a%25b",
		"too long":  "/symbols/12345678901234567",
		"non-ascii": "/symbols/%E3%83%88%E3%83%A8%E3%82%BF",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if !strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
				t.Fatalf("404 should render the error page, got body %q", rec.Body.String())
			}
		})
	}

	t.Run("16 chars is accepted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/symbols/1234567890abcdef", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestNew_RootRouteServesPlaceholderHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}

	if !strings.Contains(rec.Body.String(), "pitha-trador") {
		t.Fatalf("expected body to mention pitha-trador, got %q", rec.Body.String())
	}
}

func TestNew_SwaggerRouteServesStoplightElementsHTMLByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("expected text/html content type, got %q", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "elements-api") {
		t.Fatalf("expected body to embed Stoplight Elements, got %q", body)
	}
	if !strings.Contains(body, "/api/v1/openapi.json") {
		t.Fatalf("expected body to reference the OpenAPI spec URL, got %q", body)
	}
}

func TestNew_SwaggerRouteDisabledWhenSwaggerEnabledIsFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SWAGGER_ENABLED", "false")
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d when SWAGGER_ENABLED=false, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestNew_ReturnsAWailsIndependentEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	// internal/router MUST expose a plain http.Handler so that cmd/server
	// (net/http only) and cmd/desktop (Wails AssetServer.Handler) can share
	// the exact same engine without any Wails dependency leaking into this
	// package.
	var _ http.Handler = engine
}

func TestNew_APIScannerReturnsCandidatesFromWithCandidateSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	price := 2831.5
	source := handler.StaticCandidateSource{
		Items: []domain.Candidate{{Symbol: "7203", Price: price}},
		AsOf:  time.Date(2026, 9, 26, 10, 15, 0, 0, time.UTC),
	}
	engine := router.New(router.WithCandidateSource(source))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scanner", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"symbol":"7203"`) || !strings.Contains(body, `"price":2831.5`) {
		t.Fatalf("expected body to contain the injected candidate, got %q", body)
	}
}

func TestNew_ScannerPageServesFullPageOrFragmentByHXRequestHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCandidateSource{
		Items: []domain.Candidate{{Symbol: "7203", Price: 2831.5}},
		AsOf:  time.Now(),
	}
	engine := router.New(router.WithCandidateSource(source))

	fullReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	fullRec := httptest.NewRecorder()
	engine.ServeHTTP(fullRec, fullReq)
	if fullRec.Code != http.StatusOK {
		t.Fatalf("full page: expected status %d, got %d", http.StatusOK, fullRec.Code)
	}
	if !strings.Contains(strings.ToLower(fullRec.Body.String()), "<!doctype html>") {
		t.Fatalf("full page: expected document shell, got %q", fullRec.Body.String())
	}

	fragReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	fragReq.Header.Set("HX-Request", "true")
	fragRec := httptest.NewRecorder()
	engine.ServeHTTP(fragRec, fragReq)
	if fragRec.Code != http.StatusOK {
		t.Fatalf("fragment: expected status %d, got %d", http.StatusOK, fragRec.Code)
	}
	if strings.Contains(strings.ToLower(fragRec.Body.String()), "<!doctype") {
		t.Fatalf("fragment: expected no document shell for HX-Request, got %q", fragRec.Body.String())
	}
	if !strings.Contains(fragRec.Body.String(), "7203") {
		t.Fatalf("fragment: expected candidate symbol, got %q", fragRec.Body.String())
	}
}

func TestNew_StaticRouteServesVendoredAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/static/vendor/htmx.min.js", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "htmx") {
		t.Fatalf("expected vendored htmx bundle content, got %d bytes", rec.Body.Len())
	}
}

func TestNew_SystemStatusRouteDefaultsToRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/system/status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "running") {
		t.Fatalf("expected the default Running badge, got %q", rec.Body.String())
	}
}

func TestNew_SystemPauseRouteUsesWithSystemEngineOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(handler.StaticSystemEngine{State_: domain.SystemStatePaused}))

	req := httptest.NewRequest(http.MethodPost, "/system/pause", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "paused") {
		t.Fatalf("expected the Paused badge, got %q", rec.Body.String())
	}
}

func TestNew_APISystemKillRouteReturnsJSONState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(handler.StaticSystemEngine{State_: domain.SystemStateKilled}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/kill", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"state":"killed"`) {
		t.Fatalf("expected JSON state=killed, got %q", rec.Body.String())
	}
}

// fakeSymbolProvider is a minimal handler.SymbolProvider for
// router-wiring tests (router_test cannot reuse internal/web/handler's
// own unexported test fake across packages).
type fakeSymbolProvider struct {
	state     execution.SymbolState
	positions []domain.Position
}

func (f fakeSymbolProvider) State(context.Context, string) (execution.SymbolState, error) {
	return f.state, nil
}
func (f fakeSymbolProvider) Candles(context.Context, string, time.Time, time.Time) ([]domain.Snapshot, error) {
	return nil, nil
}
func (f fakeSymbolProvider) GetPosition(context.Context, int64) (domain.Position, error) {
	return domain.Position{}, repository.ErrPositionNotFound
}
func (f fakeSymbolProvider) ListPositions(context.Context, int) ([]domain.Position, error) {
	return f.positions, nil
}
func (f fakeSymbolProvider) Close(context.Context, int64, string, float64, time.Time) (domain.Position, error) {
	return domain.Position{}, repository.ErrPositionNotFound
}
func (f fakeSymbolProvider) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return nil, nil
}

func TestNew_APISymbolUsesDefaultStaticSymbolProviderWhenUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/7203", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("expected the requested symbol echoed back, got %q", rec.Body.String())
	}
}

func TestNew_APIPositionsUsesWithSymbolProviderOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := fakeSymbolProvider{positions: []domain.Position{
		{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2110, OpenedAt: time.Now().UTC()},
	}}
	engine := router.New(router.WithSymbolProvider(provider))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("expected the injected position, got %q", rec.Body.String())
	}
}

func TestNew_APISymbolReportsWithSymbolRiskParamsOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(
		router.WithSymbolProvider(fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionNone}}),
		router.WithSymbolRiskParams(handler.SymbolRiskParams{AllowedPositionPct: 1.0, StopLossPct: 0.4, TakeProfitPct: 0.9}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/7203", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"allowed_position_pct":1`) || !strings.Contains(rec.Body.String(), `"stop_loss_pct":0.4`) {
		t.Fatalf("expected the overridden risk params, got %q", rec.Body.String())
	}
}

func TestNew_ClosePositionActionRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodPost, "/positions/1/close", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// The default StaticSymbolProvider has no position 1, so this must
	// reach SymbolHandler.ClosePosition (proving the route is wired) and
	// fail with 404 rather than Gin's 404 (which has an empty body).
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

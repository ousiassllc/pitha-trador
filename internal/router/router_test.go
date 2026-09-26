package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
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

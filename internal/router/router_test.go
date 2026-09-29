package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestNew_RootRouteRedirectsToScannerDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/scanner" {
		t.Fatalf("expected redirect to /scanner, got %q", loc)
	}
}

func TestNew_SwaggerRouteServesStoplightElementsHTMLWhenSwaggerEnabledIsTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SWAGGER_ENABLED", "true")
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
	if strings.Contains(body, "unpkg.com") || strings.Contains(body, "https://") {
		t.Fatalf("expected body to load Elements same-origin, not from a CDN, got %q", body)
	}
}

func TestNew_SwaggerRouteServesVendoredElementsAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	for path, wantType := range map[string]string{
		"/static/dist/vendor/stoplight-elements/web-components.min.js": "javascript",
		"/static/dist/vendor/stoplight-elements/styles.min.css":        "css",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected status %d, got %d", path, http.StatusOK, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, wantType) {
			t.Fatalf("%s: expected content type containing %q, got %q", path, wantType, ct)
		}
	}
}

func TestNew_SwaggerRouteDisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SWAGGER_ENABLED", "")
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d when SWAGGER_ENABLED is unset, got %d", http.StatusNotFound, rec.Code)
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

// layout.Shell loads the HTMX error-toast module (issue #110); a missing
// esbuild entry would 404 it silently in the browser.
func TestNew_StaticRouteServesHTMXErrorsBundle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/dist/js/htmx-errors/pitha-htmx-errors.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestNew_StaticRouteServesFromDiskWhenEnvStaticDirIsSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/vendor", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(dir+"/vendor/htmx.min.js", []byte("// dev-mode marker, not the real bundle"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(router.EnvStaticDir, dir)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/static/vendor/htmx.min.js", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dev-mode marker") {
		t.Fatalf("expected disk-backed content to take precedence over the embedded bundle, got %q", rec.Body.String())
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

func TestNew_SystemStatusRoutesUseWithSystemEngineOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(handler.StaticSystemEngine{State_: domain.SystemStatePaused}))

	for _, tc := range []struct{ path, want string }{
		{"/system/status", "paused"},
		{"/api/v1/system/status", `"state":"paused"`},
	} {
		req := authorize(t, engine, httptest.NewRequest(http.MethodGet, tc.path, nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: expected status %d, got %d", tc.path, http.StatusOK, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("GET %s: expected %q, got %q", tc.path, tc.want, rec.Body.String())
		}
	}
}

// The Kill Switch panel drives /api/v1/system/* only; the HTMX action
// routes were unused and removed (issue #108), so they must stay gone.
func TestNew_RemovedHTMXSystemActionRoutesReturn404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	for _, path := range []string{"/system/pause", "/system/resume", "/system/kill"} {
		req := authorize(t, engine, httptest.NewRequest(http.MethodPost, path, nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestNew_PanickingRouteReturns500InsteadOfCrashing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(panickingSystemEngine{}))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/status", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// panickingSystemEngine panics from State, which SystemState middleware and
// the /system/status handler call, to exercise the router's Recovery.
type panickingSystemEngine struct{ handler.StaticSystemEngine }

func (panickingSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	panic("state exploded")
}

func TestNew_APISystemKillRouteReturnsJSONState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(handler.StaticSystemEngine{State_: domain.SystemStateKilled}))

	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/api/v1/system/kill", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"state":"killed"`) {
		t.Fatalf("expected JSON state=killed, got %q", rec.Body.String())
	}
}

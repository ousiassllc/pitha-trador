package staticroute_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	staticassets "github.com/ousiassllc/pitha-trador/static/src"
)

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

// The production esbuild output is go:embed'ed and served; sourcemaps are
// dev-only (`--watch`) so the binary neither bloats nor exposes the TS
// sources (issue #146). `make test` rebuilds via `make generate` first.
func TestEmbeddedStaticAssetsContainNoSourcemaps(t *testing.T) {
	err := fs.WalkDir(staticassets.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".map") && !strings.HasPrefix(path, "dist/vendor/") {
			t.Errorf("embedded static asset %s is a sourcemap; production esbuild output must not emit them", path)
		}
		return err
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
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

// layout.Shell's header logo (issue #238) is served from the embedded
// static/src/img; a missing go:embed target would 404 it silently.
func TestNew_StaticRouteServesLogo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/img/logo.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("body is not an SVG document: %q", rec.Body.String())
	}
}

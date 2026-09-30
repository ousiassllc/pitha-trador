package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

const testErrorPageMarker = "test-error-page"

// renderTestPage stands in for shared.RenderErrorPage (pages imports
// middleware, so these tests cannot use the real one).
func renderTestPage(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_, _ = c.Writer.WriteString("<p " + testErrorPageMarker + ">" + message + "</p>")
}

func renderingEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.Recovery(renderTestPage), middleware.NewSession(renderTestPage).Handler())
	engine.GET("/boom", func(*gin.Context) { panic("kaboom") })
	engine.POST("/act", func(c *gin.Context) { c.String(http.StatusOK, "done") })
	return engine
}

func withHeaders(req *http.Request, headers map[string]string) *http.Request {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

var (
	navigation = map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}
	htmx       = map[string]string{"Accept": "text/html", "HX-Request": "true"}
	fetchJSON  = map[string]string{"Accept": "*/*"}
)

func TestRecovery_PageNavigationGetsErrorPage(t *testing.T) {
	captureLogs(t)
	rec := do(renderingEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/boom", nil), navigation))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), testErrorPageMarker) {
		t.Fatalf("navigation panic: status=%d body=%q, want 500 with the error page", rec.Code, rec.Body.String())
	}
}

func TestRecovery_NonNavigationRequestsKeepStatusOnly(t *testing.T) {
	captureLogs(t)
	engine := renderingEngine()
	for name, headers := range map[string]map[string]string{"htmx": htmx, "fetch": fetchJSON, "no Accept": {}} {
		rec := do(engine, withHeaders(httptest.NewRequest(http.MethodGet, "/boom", nil), headers))
		if rec.Code != http.StatusInternalServerError || rec.Body.Len() != 0 {
			t.Errorf("%s: status=%d body=%q, want empty 500", name, rec.Code, rec.Body.String())
		}
	}
}

func TestRecovery_RendererPanicFallsBackToStatusOnly(t *testing.T) {
	captureLogs(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.Recovery(func(*gin.Context, int, string) { panic("render failed") }))
	engine.GET("/boom", func(*gin.Context) { panic("kaboom") })

	rec := do(engine, withHeaders(httptest.NewRequest(http.MethodGet, "/boom", nil), navigation))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestSession_NavigationRejectionGetsErrorPageKeepingRejectHeader(t *testing.T) {
	rec := do(renderingEngine(), withHeaders(httptest.NewRequest(http.MethodPost, "/act", nil), navigation))

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), testErrorPageMarker) {
		t.Fatalf("status=%d body=%q, want 403 with the error page", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(middleware.CSRFRejectHeader); got != middleware.CSRFRejectStale {
		t.Errorf("%s = %q, want %q", middleware.CSRFRejectHeader, got, middleware.CSRFRejectStale)
	}
}

func TestSession_NonNavigationRejectionKeepsPlainText(t *testing.T) {
	engine := renderingEngine()
	cases := map[string]*http.Request{
		"htmx":      withHeaders(httptest.NewRequest(http.MethodPost, "/act", nil), htmx),
		"fetch":     withHeaders(httptest.NewRequest(http.MethodPost, "/act", nil), fetchJSON),
		"api":       withHeaders(httptest.NewRequest(http.MethodPost, "/api/v1/act", nil), navigation),
		"websocket": withHeaders(httptest.NewRequest(http.MethodGet, "/act", nil), map[string]string{"Accept": "text/html", "Upgrade": "websocket"}),
	}
	for name, req := range cases {
		rec := do(engine, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), testErrorPageMarker) {
			t.Errorf("%s: got the HTML error page: %q", name, rec.Body.String())
		}
	}
}

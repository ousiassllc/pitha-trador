package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
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

func TestNew_ReturnsAWailsIndependentEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	// internal/router MUST expose a plain http.Handler so that cmd/server
	// (net/http only) and cmd/desktop (Wails AssetServer.Handler) can share
	// the exact same engine without any Wails dependency leaking into this
	// package.
	var _ http.Handler = engine
}

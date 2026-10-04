package swagger_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/swagger"
)

func TestSwaggerUI_ServesStoplightElementsHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/swagger", swagger.SwaggerUI)

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
	if !strings.Contains(body, "<elements-api") {
		t.Fatalf("expected body to embed the <elements-api> Stoplight Elements tag, got %q", body)
	}
	if !strings.Contains(body, `apiDescriptionUrl="/api/v1/openapi.json"`) {
		t.Fatalf("expected body to point apiDescriptionUrl at /api/v1/openapi.json, got %q", body)
	}
	for _, asset := range []string{
		`src="/static/dist/vendor/stoplight-elements/web-components.min.js"`,
		`href="/static/dist/vendor/stoplight-elements/styles.min.css"`,
	} {
		if !strings.Contains(body, asset) {
			t.Fatalf("expected body to load vendored asset %s, got %q", asset, body)
		}
	}
	if strings.Contains(body, "unpkg.com") || strings.Contains(body, "https://") {
		t.Fatalf("expected no third-party CDN reference, got %q", body)
	}
}

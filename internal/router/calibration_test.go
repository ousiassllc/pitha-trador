package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

func TestNew_CalibrationPageInjectsHeatmapURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/calibration", nil))

	if !strings.Contains(rec.Body.String(), `<pitha-calibration-heatmap calibration-url="/api/v1/calibration">`) {
		t.Fatalf("expected the heatmap to receive calibration-url, got %q", rec.Body.String())
	}
}

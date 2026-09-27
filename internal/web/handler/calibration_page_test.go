package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestCalibrationHandler_Page_RendersHeatmapIsland(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewCalibrationHandler(handler.StaticCalibrationSource{})
	engine := gin.New()
	engine.GET("/calibration", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/calibration", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<h1>Calibration</h1>",
		"<pitha-calibration-heatmap",
		`src="/static/dist/js/calibration-heatmap/pitha-calibration-heatmap.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q; body=%s", want, body)
		}
	}
}

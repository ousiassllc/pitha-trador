package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestNew_APICalibrationReturnsMetricsFromWithCalibrationSourceOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCalibrationSource{
		Metrics_: domain.CalibrationMetrics{
			Buckets: []domain.ConfidenceBucket{
				{Range: "0.50-0.60", DirectionAccuracy: 0.51, AvgFutureReturnPct: -0.05},
			},
			BrierScore:               0.19,
			LogLoss:                  0.52,
			ExpectedCalibrationError: 0.06,
		},
	}
	engine := router.New(router.WithCalibrationSource(source))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calibration", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"range":"0.50-0.60"`) || !strings.Contains(body, `"brier_score":0.19`) {
		t.Fatalf("expected body to contain the injected calibration metrics, got %q", body)
	}
}

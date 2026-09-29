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
				{Range: "0.50-0.60", AvgConfidence: 0.55, DirectionAccuracy: 0.51, AvgFutureReturnPct: -0.05, TradeCount: 3, TotalPnL: -1200, AvgPnLPct: -0.4},
			},
			ByDirection: []domain.DirectionMetric{
				{Direction: domain.JevDirectionLong, SampleCount: 10, DirectionAccuracy: 0.6, AvgFutureReturnPct: 0.12},
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
	for _, want := range []string{
		`"avg_confidence":0.55`, `"trade_count":3`, `"total_pnl":-1200`, `"avg_pnl_pct":-0.4`,
		`"by_direction":[{"direction":"LONG","sample_count":10,"direction_accuracy":0.6,"avg_future_return_pct":0.12}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestNew_CalibrationPageRendersHeatmapIslandAndNavLinksBetweenScannerAndCalibration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/calibration", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("expected document shell, got %q", body)
	}
	for _, want := range []string{
		"<pitha-calibration-heatmap",
		`href="/scanner"`,
		`href="/calibration"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

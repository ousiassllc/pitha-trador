package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
)

type fixedHealthAlerts struct{ alerts []domain.HealthAlert }

func (f fixedHealthAlerts) Active(context.Context) ([]domain.HealthAlert, error) {
	return f.alerts, nil
}

// issue #802: the header banner route is wired through router.New.
func TestNew_HealthAlertRouteShowsAlertsAndIsEmptyWithoutOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idle := []domain.HealthAlert{{Code: domain.HealthAlertMarketDataIdle, Since: time.Date(2026, 10, 20, 1, 5, 0, 0, time.UTC)}}
	for name, tc := range map[string]struct {
		opts []router.Option
		want string
	}{
		"alert active":    {[]router.Option{router.WithHealthAlertSource(fixedHealthAlerts{alerts: idle})}, "市況データの取得が止まっています"},
		"no source wired": {nil, ""},
		"no alerts":       {[]router.Option{router.WithHealthAlertSource(fixedHealthAlerts{})}, ""},
	} {
		engine := router.New(tc.opts...)
		req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/system/health-alerts", nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body=%s", name, rec.Code, rec.Body.String())
		}
		if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: body missing %q: %s", name, tc.want, rec.Body.String())
		}
		if tc.want == "" && strings.Contains(rec.Body.String(), "health-alert-banner") {
			t.Errorf("%s: rendered a banner: %s", name, rec.Body.String())
		}
	}
}

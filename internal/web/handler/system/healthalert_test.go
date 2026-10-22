package system_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

type fakeHealthAlerts struct {
	alerts []domain.HealthAlert
	err    error
}

func (f fakeHealthAlerts) Active(context.Context) ([]domain.HealthAlert, error) {
	return f.alerts, f.err
}

// issue #802: GET /system/health-alerts shows the active alerts with their
// start in JST, and is empty without any.
func TestHealthAlertHandler_Status(t *testing.T) {
	// 23:45Z is 08:45 JST the next day.
	kabu := domain.HealthAlert{Code: domain.HealthAlertKabuNotLoggedIn, Since: time.Date(2026, 10, 19, 23, 45, 0, 0, time.UTC)}
	tests := []struct {
		name     string
		source   system.HealthAlertSource
		want     []string
		wantNone bool
	}{
		{name: "alert active", source: fakeHealthAlerts{alerts: []domain.HealthAlert{kabu}},
			want: []string{`data-testid="health-alert-banner"`, `data-code="kabu_not_logged_in"`, "08:45から続いています"}},
		{name: "kabu unreachable", source: fakeHealthAlerts{alerts: []domain.HealthAlert{{Code: domain.HealthAlertKabuUnreachable, Since: kabu.Since}}},
			want: []string{`data-code="kabu_unreachable"`, "接続できません", "kabuステーションを起動してログインしてください", "08:45から続いています"}},
		{name: "no alerts", source: fakeHealthAlerts{}, wantNone: true},
		{name: "nil source", source: nil, wantNone: true},
		{name: "read failure shows nothing", source: fakeHealthAlerts{err: errors.New("db down")}, wantNone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.GET("/system/health-alerts", system.NewHealthAlertHandler(tt.source).Status)
			rec := serve(engine, http.MethodGet, "/system/health-alerts")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			if tt.wantNone && strings.Contains(body, "health-alert-banner") {
				t.Fatalf("rendered a banner: %s", body)
			}
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q: %s", want, body)
				}
			}
		})
	}
}

package router_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// TestNew_HeaderSSRsKillSwitchPanelFromSystemState is issue #106's
// regression test: every full page's Header renders the Kill Switch
// panel's status, the actions the server allows from that state and every
// URL the Lit component calls, so the client neither hardcodes them nor
// re-implements the transition table.
func TestNew_HeaderSSRsKillSwitchPanelFromSystemState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		state   domain.SystemState
		present []string
		absent  []string
	}{
		{domain.SystemStateRunning, []string{`status="running"`, "can-pause", "can-kill"}, []string{"can-resume"}},
		{domain.SystemStatePaused, []string{`status="paused"`, "can-resume", "can-kill"}, []string{"can-pause"}},
		{domain.SystemStateKilled, []string{`status="killed"`, "can-resume"}, []string{"can-pause", "can-kill"}},
	}
	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			engine := router.New(router.WithSystemEngine(system.StaticSystemEngine{State_: tc.state}))
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/calibration", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("GET /calibration = %d, want 200", rec.Code)
			}
			panel := panelTag(t, rec.Body.String())
			for _, want := range append(tc.present,
				`pause-url="/api/v1/system/pause"`,
				`resume-url="/api/v1/system/resume"`,
				`kill-url="/api/v1/system/kill"`,
				`status-url="/api/v1/system/status"`,
				`ws-url="/ws/system"`,
			) {
				if !strings.Contains(panel, want) {
					t.Errorf("panel %q missing %q", panel, want)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(panel, unwanted) {
					t.Errorf("panel %q must not contain %q", panel, unwanted)
				}
			}
		})
	}
}

func panelTag(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<pitha-kill-switch-panel")
	if start < 0 {
		t.Fatalf("no <pitha-kill-switch-panel> in %q", body)
	}
	end := strings.Index(body[start:], ">")
	return body[start : start+end+1]
}

func TestNew_APISystemStatusReportsAllowedActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(system.StaticSystemEngine{State_: domain.SystemStatePaused}))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))

	for _, want := range []string{`"state":"paused"`, `"can_pause":false`, `"can_resume":true`, `"can_kill":true`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body %q missing %s", rec.Body.String(), want)
		}
	}
}

type failingStateEngine struct{ system.StaticSystemEngine }

func (failingStateEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return "", nil, errors.New("state unavailable")
}

// A Header rendered while the state cannot be read must not offer any
// action (nor claim "running"): the panel resyncs itself, and
// #header-status fetches its badge on load.
func TestNew_HeaderRendersNoActionsWhenSystemStateUnreadable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(failingStateEngine{}))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/calibration", nil))

	panel := panelTag(t, rec.Body.String())
	for _, unwanted := range []string{"can-pause", "can-resume", "can-kill", `status="running"`} {
		if strings.Contains(panel, unwanted) {
			t.Errorf("panel %q must not contain %q", panel, unwanted)
		}
	}
	if !strings.Contains(rec.Body.String(), `hx-trigger="load, systemStateChanged from:closest header"`) {
		t.Errorf("#header-status must self-correct on load, got %q", rec.Body.String())
	}
}

// #header-status is refreshed by systemStateChanged (panel push/resync/
// action), never by the operator directly, so its request must carry the
// background marker or it would extend the dead-man's switch (FR-RISK-6).
func TestNew_HeaderStatusRefreshIsMarkedBackground(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(failingStateEngine{}))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/calibration", nil))

	want := `hx-headers="{&#34;` + middleware.BackgroundHeader + `&#34;:&#34;1&#34;}"`
	alt := `hx-headers='{"` + middleware.BackgroundHeader + `":"1"}'`
	if body := rec.Body.String(); !strings.Contains(body, want) && !strings.Contains(body, alt) {
		t.Errorf("#header-status must send %s, got %q", middleware.BackgroundHeader, body)
	}
}

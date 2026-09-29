package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

type countingRecorder struct{ calls int }

func (r *countingRecorder) RecordHeartbeat(context.Context, time.Time) error {
	r.calls++
	return nil
}

func TestNew_WithHeartbeatRecorderRecordsAuthenticatedPageAndActionRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &countingRecorder{}
	engine := router.New(router.WithHeartbeatRecorder(recorder))

	// authorize's own bootstrap GET carries no session cookie yet.
	req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/scanner", nil))
	if recorder.calls != 0 {
		t.Fatalf("cookie-less bootstrap request recorded %d heartbeats, want 0", recorder.calls)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/scanner"},
		{http.MethodGet, "/api/v1/scanner"},
		{http.MethodPost, "/api/v1/system/pause"},
	} {
		before := recorder.calls
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header = req.Header.Clone() // session cookie + CSRF token
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, r)
		if recorder.calls != before+1 {
			t.Errorf("%s %s (status %d) recorded %d heartbeats, want 1", tc.method, tc.path, rec.Code, recorder.calls-before)
		}
	}
}

func TestNew_WithHeartbeatRecorderSkipsNonOperatorTraffic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &countingRecorder{}
	engine := router.New(router.WithHeartbeatRecorder(recorder))
	authed := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/scanner", nil))

	for _, path := range []string{"/system/update-status", "/static/dist/app.js"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(authed.Cookies()[0])
		engine.ServeHTTP(httptest.NewRecorder(), r)
	}
	ws := httptest.NewRequest(http.MethodGet, "/ws/system", nil)
	ws.Header.Set("Upgrade", "websocket")
	ws.AddCookie(authed.Cookies()[0])
	engine.ServeHTTP(httptest.NewRecorder(), ws)

	// Unauthenticated (no cookie) page GET.
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/scanner", nil))

	if recorder.calls != 0 {
		t.Fatalf("recorded %d heartbeats for polls/static/websocket/unauthenticated traffic, want 0", recorder.calls)
	}
}

// TestNew_LiveDeadMansSwitchStaysQuietWhileOperatorUsesTheUI wires the real
// risk.Engine with Live's 120 minute timeout (issues #89/#100): a UI
// request keeps CheckHeartbeatTimeout from raising operator_heartbeat_timeout,
// and 120+ idle minutes afterwards raise it.
func TestNew_LiveDeadMansSwitchStaysQuietWhileOperatorUsesTheUI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := repository.Open(filepath.Join(t.TempDir(), "router_heartbeat.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clock := time.Now()
	engineRisk := risk.NewEngine(risk.Config{
		Limits:     config.RiskLimits{HeartbeatTimeoutMinutes: 120},
		KillSwitch: repository.NewKillSwitchRepository(db),
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Now:        func() time.Time { return clock },
	})
	engine := router.New(router.WithSystemEngine(engineRisk), router.WithHeartbeatRecorder(engineRisk))
	ctx := context.Background()

	state := func() domain.SystemState {
		t.Helper()
		s, _, err := engineRisk.State(ctx)
		if err != nil {
			t.Fatalf("State: %v", err)
		}
		return s
	}

	authed := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/scanner", nil))
	r := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	r.AddCookie(authed.Cookies()[0])
	engine.ServeHTTP(httptest.NewRecorder(), r)

	clock = clock.Add(119 * time.Minute)
	if err := engineRisk.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	if got := state(); got != domain.SystemStateRunning {
		t.Fatalf("state 119m after a UI request = %q, want %q", got, domain.SystemStateRunning)
	}

	clock = clock.Add(2 * time.Minute)
	if err := engineRisk.CheckHeartbeatTimeout(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	if got := state(); got != domain.SystemStateKilled {
		t.Fatalf("state 121m after the last UI request = %q, want %q", got, domain.SystemStateKilled)
	}
}

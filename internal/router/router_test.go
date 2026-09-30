package router_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

func TestNew_RootRouteRedirectsToScannerDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/scanner" {
		t.Fatalf("expected redirect to /scanner, got %q", loc)
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

func TestNew_APIScannerReturnsCandidatesFromWithCandidateSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	price := 2831.5
	source := handler.StaticCandidateSource{
		Items: []domain.Candidate{{Symbol: "7203", Price: price}},
		AsOf:  time.Date(2026, 9, 26, 10, 15, 0, 0, time.UTC),
	}
	engine := router.New(router.WithCandidateSource(source))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scanner", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"symbol":"7203"`) || !strings.Contains(body, `"price":2831.5`) {
		t.Fatalf("expected body to contain the injected candidate, got %q", body)
	}
}

func TestNew_ScannerPageServesFullPageOrFragmentByHXRequestHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCandidateSource{
		Items: []domain.Candidate{{Symbol: "7203", Price: 2831.5}},
		AsOf:  time.Now(),
	}
	engine := router.New(router.WithCandidateSource(source))

	fullReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	fullRec := httptest.NewRecorder()
	engine.ServeHTTP(fullRec, fullReq)
	if fullRec.Code != http.StatusOK {
		t.Fatalf("full page: expected status %d, got %d", http.StatusOK, fullRec.Code)
	}
	if !strings.Contains(strings.ToLower(fullRec.Body.String()), "<!doctype html>") {
		t.Fatalf("full page: expected document shell, got %q", fullRec.Body.String())
	}

	fragReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	fragReq.Header.Set("HX-Request", "true")
	fragRec := httptest.NewRecorder()
	engine.ServeHTTP(fragRec, fragReq)
	if fragRec.Code != http.StatusOK {
		t.Fatalf("fragment: expected status %d, got %d", http.StatusOK, fragRec.Code)
	}
	if strings.Contains(strings.ToLower(fragRec.Body.String()), "<!doctype") {
		t.Fatalf("fragment: expected no document shell for HX-Request, got %q", fragRec.Body.String())
	}
	if !strings.Contains(fragRec.Body.String(), "7203") {
		t.Fatalf("fragment: expected candidate symbol, got %q", fragRec.Body.String())
	}
}

func TestNew_SystemStatusRouteDefaultsToRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/system/status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "running") {
		t.Fatalf("expected the default Running badge, got %q", rec.Body.String())
	}
}

func TestNew_SystemStatusRoutesUseWithSystemEngineOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(system.StaticSystemEngine{State_: domain.SystemStatePaused}))

	for _, tc := range []struct{ path, want string }{
		{"/system/status", "paused"},
		{"/api/v1/system/status", `"state":"paused"`},
	} {
		req := authorize(t, engine, httptest.NewRequest(http.MethodGet, tc.path, nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: expected status %d, got %d", tc.path, http.StatusOK, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("GET %s: expected %q, got %q", tc.path, tc.want, rec.Body.String())
		}
	}
}

// The Kill Switch panel drives /api/v1/system/* only; the HTMX action
// routes were unused and removed (issue #108), so they must stay gone.
func TestNew_RemovedHTMXSystemActionRoutesReturn404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	for _, path := range []string{"/system/pause", "/system/resume", "/system/kill"} {
		req := authorize(t, engine, httptest.NewRequest(http.MethodPost, path, nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestNew_PanickingRouteReturns500InsteadOfCrashing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(panickingSystemEngine{}))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/status", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// panickingSystemEngine panics from State, which SystemState middleware and
// the /system/status handler call, to exercise the router's Recovery.
type panickingSystemEngine struct{ system.StaticSystemEngine }

func (panickingSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	panic("state exploded")
}

func TestNew_APISystemKillRouteReturnsJSONState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(system.StaticSystemEngine{State_: domain.SystemStateKilled}))

	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/api/v1/system/kill", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"state":"killed"`) {
		t.Fatalf("expected JSON state=killed, got %q", rec.Body.String())
	}
}

type failingSystemEngine struct{ system.StaticSystemEngine }

func (failingSystemEngine) Kill(context.Context) error {
	return errors.New("sqlite: disk I/O error at /var/lib/pitha/secret.db")
}

// TestNew_APIInternalErrorHidesCause is issue #215's regression test: a
// failing engine must not reach the client through errors[].message, and the
// cause must land in slog.
func TestNew_APIInternalErrorHidesCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	engine := router.New(router.WithSystemEngine(failingSystemEngine{}))

	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/api/v1/system/kill", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret.db") || strings.Contains(rec.Body.String(), "sqlite") {
		t.Fatalf("response leaks cause: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "secret.db") {
		t.Fatalf("cause missing from slog: %q", logs.String())
	}
}

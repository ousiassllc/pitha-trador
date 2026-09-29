package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

type fakeRecorder struct {
	calls int
	last  time.Time
	err   error
}

func (f *fakeRecorder) RecordHeartbeat(_ context.Context, at time.Time) error {
	f.calls++
	f.last = at
	return f.err
}

// heartbeatEngine serves the routes Heartbeat must (and must not) count,
// behind Session.Handler like internal/router does.
func heartbeatEngine(t *testing.T, recorder middleware.HeartbeatRecorder) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.NewSession().Handler())
	engine.Use(middleware.Heartbeat(recorder))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	engine.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, middleware.CSRFToken(c.Request.Context())) })
	engine.GET("/ws", ok)
	engine.GET("/static/app.js", ok)
	engine.GET("/system/update-status", ok)
	engine.POST("/act", ok)
	return engine
}

func TestHeartbeat_RecordsAuthenticatedRequestsAndCallsNext(t *testing.T) {
	recorder := &fakeRecorder{}
	engine := heartbeatEngine(t, recorder)
	cookie, csrf := login(t, engine)
	if recorder.calls != 0 {
		t.Fatalf("first request (no cookie yet) recorded %d heartbeats, want 0", recorder.calls)
	}

	rec := do(engine, request(http.MethodGet, "/page", cookie, ""))
	if rec.Code != http.StatusOK || recorder.calls != 1 {
		t.Fatalf("GET /page = %d with %d heartbeats, want 200 and 1", rec.Code, recorder.calls)
	}
	if time.Since(recorder.last) > time.Minute {
		t.Fatalf("recorded heartbeat time %v is not now", recorder.last)
	}

	rec = do(engine, request(http.MethodPost, "/act", cookie, csrf))
	if rec.Code != http.StatusOK || recorder.calls != 2 {
		t.Fatalf("POST /act = %d with %d heartbeats, want 200 and 2", rec.Code, recorder.calls)
	}
}

func TestHeartbeat_IgnoresRequestsThatAreNotOperatorActivity(t *testing.T) {
	recorder := &fakeRecorder{}
	engine := heartbeatEngine(t, recorder)
	cookie, _ := login(t, engine)

	staleCookie := &http.Cookie{Name: middleware.SessionCookieName, Value: "stale"}
	wsUpgrade := request(http.MethodGet, "/ws", cookie, "")
	wsUpgrade.Header.Set("Upgrade", "websocket")

	for name, req := range map[string]*http.Request{
		"no session cookie":      request(http.MethodGet, "/page", nil, ""),
		"invalid session cookie": request(http.MethodGet, "/page", staleCookie, ""),
		"static asset":           request(http.MethodGet, "/static/app.js", cookie, ""),
		"websocket upgrade":      wsUpgrade,
		"background poll":        request(http.MethodGet, "/system/update-status", cookie, ""),
	} {
		rec := do(engine, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (skipping a heartbeat must not affect the request)", name, rec.Code)
		}
		if recorder.calls != 0 {
			t.Fatalf("%s recorded %d heartbeats, want 0", name, recorder.calls)
		}
	}
}

func TestHeartbeat_RecordFailureDoesNotBlockRequest(t *testing.T) {
	recorder := &fakeRecorder{err: errors.New("db unavailable")}
	engine := heartbeatEngine(t, recorder)
	cookie, _ := login(t, engine)

	rec := do(engine, request(http.MethodGet, "/page", cookie, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a heartbeat write error must not fail the request)", rec.Code, http.StatusOK)
	}
	if recorder.calls != 1 {
		t.Fatalf("RecordHeartbeat calls = %d, want 1", recorder.calls)
	}
}

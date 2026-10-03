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
	// Zero interval: every counted request is recorded, so these tests
	// observe classification (what counts) independent of throttling.
	return heartbeatEngineWith(t, middleware.NewHeartbeatWithClock(recorder, 0, time.Now))
}

func heartbeatEngineWith(t *testing.T, heartbeat gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.NewSession(nil).Handler())
	engine.Use(heartbeat)
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	engine.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, middleware.CSRFToken(c.Request.Context())) })
	engine.GET("/ws", ok)
	engine.GET("/static/app.js", ok)
	engine.GET("/system/update-status", ok)
	engine.GET("/system/marketdata-status", ok)
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
	// Auto-fired resync (kill_switch push / WS reconnect / header refresh).
	resync := request(http.MethodGet, "/page", cookie, "")
	resync.Header.Set(middleware.BackgroundHeader, "1")

	for name, req := range map[string]*http.Request{
		"no session cookie":      request(http.MethodGet, "/page", nil, ""),
		"invalid session cookie": request(http.MethodGet, "/page", staleCookie, ""),
		"static asset":           request(http.MethodGet, "/static/app.js", cookie, ""),
		"websocket upgrade":      wsUpgrade,
		"background poll":        request(http.MethodGet, "/system/update-status", cookie, ""),
		"marketdata poll":        request(http.MethodGet, "/system/marketdata-status", cookie, ""),
		"background header":      resync,
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

func TestHeartbeat_ThrottlesWritesWithinInterval(t *testing.T) {
	recorder := &fakeRecorder{}
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	engine := heartbeatEngineWith(t, middleware.NewHeartbeatWithClock(recorder, 10*time.Second, func() time.Time { return now }))
	cookie, _ := login(t, engine)

	get := func() {
		if rec := do(engine, request(http.MethodGet, "/page", cookie, "")); rec.Code != http.StatusOK {
			t.Fatalf("GET /page = %d, want 200", rec.Code)
		}
	}

	get()
	now = now.Add(9 * time.Second)
	get()
	if recorder.calls != 1 {
		t.Fatalf("heartbeats within the interval = %d, want 1 (throttled)", recorder.calls)
	}

	now = now.Add(time.Second)
	get()
	if recorder.calls != 2 || !recorder.last.Equal(now) {
		t.Fatalf("after the interval: calls=%d last=%v, want 2 and %v", recorder.calls, recorder.last, now)
	}
}

func TestHeartbeat_FailedWriteIsRetriedByNextRequest(t *testing.T) {
	recorder := &fakeRecorder{err: errors.New("db unavailable")}
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	engine := heartbeatEngineWith(t, middleware.NewHeartbeatWithClock(recorder, time.Minute, func() time.Time { return now }))
	cookie, _ := login(t, engine)

	do(engine, request(http.MethodGet, "/page", cookie, ""))
	do(engine, request(http.MethodGet, "/page", cookie, ""))

	if recorder.calls != 2 {
		t.Fatalf("RecordHeartbeat calls = %d, want 2 (a failed write must not start the throttle window)", recorder.calls)
	}
}

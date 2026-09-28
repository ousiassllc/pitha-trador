package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestHeartbeat_RecordsOnEveryRequestAndCallsNext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &fakeRecorder{}
	engine := gin.New()
	engine.Use(middleware.Heartbeat(recorder))
	nextCalled := false
	engine.GET("/", func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if recorder.calls != 1 {
		t.Fatalf("RecordHeartbeat calls = %d, want 1", recorder.calls)
	}
	if !nextCalled {
		t.Fatalf("next handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHeartbeat_RecordFailureDoesNotBlockRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &fakeRecorder{err: errors.New("db unavailable")}
	engine := gin.New()
	engine.Use(middleware.Heartbeat(recorder))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a heartbeat write error must not fail the request)", rec.Code, http.StatusOK)
	}
	if recorder.calls != 1 {
		t.Fatalf("RecordHeartbeat calls = %d, want 1", recorder.calls)
	}
}

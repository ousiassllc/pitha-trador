package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

type fakeStateReader struct {
	state domain.SystemState
	err   error
	calls int
}

func (f *fakeStateReader) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	f.calls++
	return f.state, nil, f.err
}

func serveSystemState(t *testing.T, reader *fakeStateReader, read func(ctx context.Context)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SystemState(reader))
	engine.GET("/page", func(c *gin.Context) {
		read(c.Request.Context())
		c.Status(http.StatusOK)
	})
	engine.GET("/fragment", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/page", nil))
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fragment", nil))
}

func TestSystemState_ExposesStateLazilyToTemplates(t *testing.T) {
	reader := &fakeStateReader{state: domain.SystemStatePaused}
	var got domain.SystemState
	serveSystemState(t, reader, func(ctx context.Context) { got = middleware.SystemStateFrom(ctx) })

	if got != domain.SystemStatePaused {
		t.Errorf("SystemStateFrom = %q, want paused", got)
	}
	if reader.calls != 1 {
		t.Errorf("State read %d times, want 1 (only the request that renders it)", reader.calls)
	}
}

func TestSystemState_UnreadableStateIsUnknown(t *testing.T) {
	reader := &fakeStateReader{state: domain.SystemStateKilled, err: errors.New("db down")}
	got := domain.SystemStateRunning
	serveSystemState(t, reader, func(ctx context.Context) { got = middleware.SystemStateFrom(ctx) })

	if got != "" {
		t.Errorf("SystemStateFrom on read error = %q, want unknown (\"\")", got)
	}
}

func TestSystemStateFrom_WithoutMiddlewareIsUnknown(t *testing.T) {
	if got := middleware.SystemStateFrom(context.Background()); got != "" {
		t.Errorf("SystemStateFrom = %q, want \"\"", got)
	}
}

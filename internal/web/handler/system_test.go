package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// fakeSystemEngine is a configurable handler.SystemEngine for tests. Each
// call is recorded so tests can assert the handler invoked the right
// Engine method.
type fakeSystemEngine struct {
	state       domain.SystemState
	events      []domain.KillSwitchEvent
	stateErr    error
	pauseCalls  int
	resumeCalls int
	killCalls   int
	opErr       error
}

func (f *fakeSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return f.state, f.events, f.stateErr
}
func (f *fakeSystemEngine) Pause(context.Context) error  { f.pauseCalls++; return f.opErr }
func (f *fakeSystemEngine) Resume(context.Context) error { f.resumeCalls++; return f.opErr }
func (f *fakeSystemEngine) Kill(context.Context) error   { f.killCalls++; return f.opErr }

func TestSystemHandler_Pause_CallsEngineAndRendersBadge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStatePaused}
	h := handler.NewSystemHandler(engine)
	router := gin.New()
	router.POST("/system/pause", h.Pause)

	req := httptest.NewRequest(http.MethodPost, "/system/pause", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if engine.pauseCalls != 1 {
		t.Fatalf("Pause calls = %d, want 1", engine.pauseCalls)
	}
	if !strings.Contains(rec.Body.String(), "paused") {
		t.Fatalf("body = %q, want it to contain the paused state", rec.Body.String())
	}
}

func TestSystemHandler_Resume_CallsEngineAndRendersRunningBadge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStateRunning}
	h := handler.NewSystemHandler(engine)
	router := gin.New()
	router.POST("/system/resume", h.Resume)

	req := httptest.NewRequest(http.MethodPost, "/system/resume", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if engine.resumeCalls != 1 {
		t.Fatalf("Resume calls = %d, want 1", engine.resumeCalls)
	}
	if !strings.Contains(rec.Body.String(), "running") {
		t.Fatalf("body = %q, want it to contain the running state", rec.Body.String())
	}
}

func TestSystemHandler_Kill_CallsEngineAndRendersKilledBadge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStateKilled}
	h := handler.NewSystemHandler(engine)
	router := gin.New()
	router.POST("/system/kill", h.Kill)

	req := httptest.NewRequest(http.MethodPost, "/system/kill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if engine.killCalls != 1 {
		t.Fatalf("Kill calls = %d, want 1", engine.killCalls)
	}
	if !strings.Contains(rec.Body.String(), "killed") {
		t.Fatalf("body = %q, want it to contain the killed state", rec.Body.String())
	}
}

func TestSystemHandler_Status_DoesNotMutateState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStateRunning}
	h := handler.NewSystemHandler(engine)
	router := gin.New()
	router.GET("/system/status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if engine.pauseCalls != 0 || engine.resumeCalls != 0 || engine.killCalls != 0 {
		t.Fatalf("Status must not call Pause/Resume/Kill: %+v", engine)
	}
}

func TestSystemHandler_Pause_EngineErrorReturns500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{opErr: errors.New("db unavailable")}
	h := handler.NewSystemHandler(engine)
	router := gin.New()
	router.POST("/system/pause", h.Pause)

	req := httptest.NewRequest(http.MethodPost, "/system/pause", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestSystemHandler_APIPause_ReturnsCurrentState(t *testing.T) {
	engine := &fakeSystemEngine{state: domain.SystemStatePaused}
	h := handler.NewSystemHandler(engine)
	_, api := humatest.New(t)
	huma.Post(api, "/system/pause", h.APIPause)

	resp := api.Post("/system/pause")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}
	if body.State != string(domain.SystemStatePaused) {
		t.Fatalf("state = %q, want %q", body.State, domain.SystemStatePaused)
	}
	if engine.pauseCalls != 1 {
		t.Fatalf("Pause calls = %d, want 1", engine.pauseCalls)
	}
}

func TestSystemHandler_APIKill_EngineErrorReturnsProblemDetails(t *testing.T) {
	engine := &fakeSystemEngine{opErr: errors.New("db unavailable")}
	h := handler.NewSystemHandler(engine)
	_, api := humatest.New(t)
	huma.Post(api, "/system/kill", h.APIKill)

	resp := api.Post("/system/kill")

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusInternalServerError, resp.Body.String())
	}
}

func TestStaticSystemEngine_DefaultsToRunning(t *testing.T) {
	state, events, err := (handler.StaticSystemEngine{}).State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateRunning {
		t.Fatalf("State = %q, want %q", state, domain.SystemStateRunning)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want empty", events)
	}
	if err := (handler.StaticSystemEngine{}).Pause(context.Background()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
}

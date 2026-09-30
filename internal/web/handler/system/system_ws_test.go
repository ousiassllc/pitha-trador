package system_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

// transitioningSystemEngine reports domain.SystemStateRunning for its
// first call and domain.SystemStateKilled for every call after - a
// minimal fake for testing WebSocket's transition-triggered push
// (fakeSystemEngine's fixed `state` field cannot change mid-test).
type transitioningSystemEngine struct {
	calls  atomic.Int64
	reason string
}

func (f *transitioningSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	if f.calls.Add(1) == 1 {
		return domain.SystemStateRunning, nil, nil
	}
	return domain.SystemStateKilled, []domain.KillSwitchEvent{{Reason: f.reason}}, nil
}
func (f *transitioningSystemEngine) Pause(context.Context) error  { return nil }
func (f *transitioningSystemEngine) Resume(context.Context) error { return nil }
func (f *transitioningSystemEngine) Kill(context.Context) error   { return nil }

func TestSystemHandler_WebSocket_PushesKillSwitchOnTransition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &transitioningSystemEngine{reason: domain.KillReasonDailyLossLimit}
	h := system.NewSystemHandler(engine)
	h.SetPollInterval(20 * time.Millisecond)

	ginEngine := gin.New()
	ginEngine.GET("/ws/system", h.WebSocket)
	server := httptest.NewServer(ginEngine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/system"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var got struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, data = %s", err, data)
	}
	if got.Type != "kill_switch" || got.Reason != domain.KillReasonDailyLossLimit {
		t.Fatalf("message = %+v, want Type=kill_switch Reason=%q", got, domain.KillReasonDailyLossLimit)
	}
}

func TestSystemHandler_WebSocket_ManualKillReportsManualReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStateKilled, events: nil}
	h := system.NewSystemHandler(engine)
	h.SetPollInterval(20 * time.Millisecond)

	ginEngine := gin.New()
	ginEngine.GET("/ws/system", h.WebSocket)
	server := httptest.NewServer(ginEngine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/system"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var got struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, data = %s", err, data)
	}
	if got.Type != "kill_switch" || got.Reason != "manual" {
		t.Fatalf("message = %+v, want Type=kill_switch Reason=manual (no kill_switch_events row)", got)
	}
}

func TestSystemHandler_WebSocket_NoPushWhileRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &fakeSystemEngine{state: domain.SystemStateRunning}
	h := system.NewSystemHandler(engine)
	h.SetPollInterval(20 * time.Millisecond)

	ginEngine := gin.New()
	ginEngine.GET("/ws/system", h.WebSocket)
	server := httptest.NewServer(ginEngine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/system"
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatalf("conn.Read() succeeded, want a timeout (no message while state stays running)")
	}
}

// #178: the status badge's 500 logs the engine error via slog.
func TestSystemHandler_Status_500LogsCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	router := gin.New()
	router.GET("/system/status", system.NewSystemHandler(&fakeSystemEngine{stateErr: errors.New("state cause")}).Status)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/status", nil))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(logs.String(), "state cause") {
		t.Errorf("status = %d, log = %q; want 500 with the cause logged", rec.Code, logs.String())
	}
}

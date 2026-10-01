package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// Issue #266: the desktop app serves WebSockets from a separate listener;
// pages must tell the Lit components where it is.
func TestNew_WithWebSocketBaseRendersWsBaseMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithWebSocketBase("ws://wails.localhost:51234"), router.WithAllowedHosts(middleware.WailsHosts()...))

	req := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	req.Host = "wails.localhost"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<meta name="ws-base" content="ws://wails.localhost:51234"`) {
		t.Fatalf("GET /scanner = %d, want 200 with the ws-base meta; body = %.300s", rec.Code, rec.Body.String())
	}
}

func TestNew_WithoutWebSocketBaseRendersNoWsBaseMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))

	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `name="ws-base"`) {
		t.Fatalf("GET /scanner = %d, want 200 without a ws-base meta", rec.Code)
	}
}

// The desktop's extra listener must expose WebSocket upgrades only: not the
// pages, and not the API.
func TestWebSocketOnly(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	handler := router.WebSocketOnly(next)

	tests := []struct {
		name, path, upgrade string
		wantReached         bool
	}{
		{"websocket upgrade of a ws route", "/ws/scanner", "websocket", true},
		{"upgrade header is case-insensitive", "/ws/system", "WebSocket", true},
		{"plain GET of a ws route", "/ws/scanner", "", false},
		{"upgrade of a page", "/scanner", "websocket", false},
		{"plain GET of the API", "/api/v1/system/state", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if reached != tt.wantReached {
				t.Fatalf("reached next = %v, want %v (status %d)", reached, tt.wantReached, rec.Code)
			}
			if !tt.wantReached && rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// Package wslistener_test holds the integration test of the desktop's
// separate `/ws` listener (router.WebSocketOnly). It only uses router's
// exported API and lives in its own directory to keep internal/router under
// the linterly line budget (as router/analysisflow does, #248).
package wslistener_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// Issue #286: the separate listener must enforce HostGuard and Session on
// `/ws/...` exactly like the AssetServer-fronted HTTP routes: the page is
// served on Host `wails.localhost` (Origin `http://wails.localhost`) while
// the socket connects to `wails.localhost:<port>`.
func TestWebSocketListener_EnforcesHostGuardAndSessionLikeHTTPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// cmd/desktop learns the port only after listening, so the listener
	// starts first and the engine is wired to it afterwards.
	var engine *gin.Engine
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.WebSocketOnly(engine).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := u.Port()
	engine = router.New(router.WithWebSocketBase("ws://wails.localhost:"+port), router.WithAllowedHosts(middleware.WailsHosts()...))

	page := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	pageReq.Host = "wails.localhost"
	engine.ServeHTTP(page, pageReq)
	cookies := page.Result().Cookies()
	if page.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("GET /scanner = %d cookies=%v, want 200 with the session cookie", page.Code, cookies)
	}
	session := cookies[0].Name + "=" + cookies[0].Value

	tests := []struct {
		name, host, origin, cookie string
		wantStatus                 int // 0: the upgrade succeeds
	}{
		{"cookie, wails host and origin", "wails.localhost:" + port, "http://wails.localhost", session, 0},
		{"origin carrying the listener port", "wails.localhost:" + port, "http://wails.localhost:" + port, session, 0},
		{"no origin (non-browser client)", "wails.localhost:" + port, "", session, 0},
		{"no cookie", "wails.localhost:" + port, "http://wails.localhost", "", http.StatusForbidden},
		{"forged cookie", "wails.localhost:" + port, "http://wails.localhost", "pitha_session=forged", http.StatusForbidden},
		{"foreign origin", "wails.localhost:" + port, "http://evil.example", session, http.StatusForbidden},
		{"null origin", "wails.localhost:" + port, "null", session, http.StatusForbidden},
		{"rebound host", "evil.example:" + port, "http://evil.example:" + port, session, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			header := http.Header{}
			if tt.origin != "" {
				header.Set("Origin", tt.origin)
			}
			if tt.cookie != "" {
				header.Set("Cookie", tt.cookie)
			}
			conn, resp, err := websocket.Dial(ctx, "ws://127.0.0.1:"+port+"/ws/system", &websocket.DialOptions{Host: tt.host, HTTPHeader: header})
			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("upgrade failed: %v (response %v), want success", err, resp)
				}
				_ = conn.CloseNow()
				return
			}
			if err == nil {
				_ = conn.CloseNow()
				t.Fatalf("upgrade succeeded, want %d", tt.wantStatus)
			}
			if resp == nil || resp.StatusCode != tt.wantStatus {
				t.Fatalf("upgrade status = %v (err %v), want %d", resp, err, tt.wantStatus)
			}
		})
	}
}

// cmd/server has no ws-base: the port-less Origin allowance must not leak
// there, where another local service on `localhost:<other port>` must not
// be able to open the socket with the cookie it is sent.
func TestWebSocket_WithoutWsBaseRefusesOriginOfAnotherPort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithAllowedHosts(middleware.LoopbackHosts()...))
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	pageReq.Host = "localhost:" + u.Port()
	engine.ServeHTTP(page, pageReq)
	cookies := page.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("GET /scanner cookies = %v, want the session cookie", cookies)
	}

	for origin, wantOK := range map[string]bool{
		"http://localhost:" + u.Port(): true,
		"http://localhost:9":           false,
		"http://localhost":             false,
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		header := http.Header{"Origin": {origin}, "Cookie": {cookies[0].Name + "=" + cookies[0].Value}}
		conn, _, err := websocket.Dial(ctx, "ws://"+u.Host+"/ws/system", &websocket.DialOptions{Host: "localhost:" + u.Port(), HTTPHeader: header})
		cancel()
		if err == nil {
			_ = conn.CloseNow()
		}
		if (err == nil) != wantOK {
			t.Errorf("Origin %s: upgrade err = %v, want success = %v", origin, err, wantOK)
		}
	}
}

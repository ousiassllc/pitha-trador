package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// listenWebSocket reserves a random loopback port for the WebSocket-only
// listener and returns it together with the WebSocket base URL the pages
// must use ("" and a nil listener when the listener is not needed or could
// not be opened).
//
// The Wails AssetServer cannot carry WebSockets (it answers upgrades with
// 501, and WebView2 does not even route `ws://` requests to it), so without
// this listener every `pitha-*` component's socket fails and the pages show
// "接続が切れています" permanently (issue #266). The base host is the
// WebView's own `wails.localhost` so the HttpOnly SameSite=Strict session
// cookie (cookies ignore ports) and the Origin check of middleware.HostGuard
// keep working; `*.localhost` resolves to loopback in WebView2. That is only
// the Windows/WebView2 host: elsewhere (`wails://wails/`) there is no
// listener and the pages keep using their own origin.
func listenWebSocket() (ln net.Listener, base string) {
	if runtime.GOOS != "windows" {
		return nil, ""
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("desktop: websocket listener failed; live updates will not work", "error", err)
		return nil, ""
	}
	return ln, fmt.Sprintf("ws://wails.localhost:%d", ln.Addr().(*net.TCPAddr).Port)
}

// serveWebSocket serves engine's `/ws/...` routes (and nothing else,
// router.WebSocketOnly) on ln until the returned stop function is called.
func serveWebSocket(ln net.Listener, engine http.Handler) (stop func()) {
	srv := &http.Server{Handler: router.WebSocketOnly(engine), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("desktop: websocket listener stopped", "error", err)
		}
	}()
	return func() { _ = srv.Close() }
}

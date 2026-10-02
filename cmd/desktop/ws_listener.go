package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// listenWebSocket reserves a random loopback port for the WebSocket-only
// listener and returns its listeners (127.0.0.1 and, where IPv6 loopback
// exists, [::1] on the same port) together with the WebSocket base URL the
// pages must use (nil and "" when the listener is not needed or could not
// be opened).
//
// The Wails AssetServer cannot carry WebSockets, so without this listener
// every `pitha-*` component's socket fails and the pages show
// "接続が切れています" permanently (issue #266). On Windows, WebView2 does
// not route `ws://` requests to the AssetServer at all (it only hands it
// `http(s)://wails.localhost/...`), so the request goes to the network and
// fails when nothing listens. The base host is the WebView's own
// `wails.localhost` so the HttpOnly SameSite=Strict session cookie (cookies
// ignore ports) and the Origin check of middleware.HostGuard keep working;
// `*.localhost` resolves to loopback in WebView2, possibly to ::1, which is
// why both loopback addresses are bound: otherwise another local process
// could take `[::1]:<port>` and receive the session cookie of the upgrade
// (issue #285). That is only the Windows/WebView2 host: elsewhere
// (`wails://wails/`) there is no listener and the pages keep using their
// own origin.
func listenWebSocket() (lns []net.Listener, base string) {
	if runtime.GOOS != "windows" {
		return nil, ""
	}
	lns, port, err := listenLoopback(net.Listen, loopbackAttempts)
	if err != nil {
		slog.Error("desktop: websocket listener failed; live updates will not work", "error", err)
		return nil, ""
	}
	return lns, fmt.Sprintf("ws://wails.localhost:%d", port)
}

// loopbackAttempts bounds how often listenLoopback retries with a new port
// when the chosen one is taken on ::1.
const loopbackAttempts = 10

type listenFunc func(network, address string) (net.Listener, error)

// listenLoopback listens on a random free 127.0.0.1 port and on the same
// port of [::1]. A port taken on ::1 is retried with a fresh one up to
// attempts times; a host without IPv6 loopback gets the IPv4 listener only.
func listenLoopback(listen listenFunc, attempts int) (lns []net.Listener, port int, err error) {
	for range attempts {
		v4, err := listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, 0, err
		}
		port = v4.Addr().(*net.TCPAddr).Port
		v6, err := listen("tcp", net.JoinHostPort("::1", strconv.Itoa(port)))
		if err == nil {
			return []net.Listener{v4, v6}, port, nil
		}
		if !ipv6LoopbackAvailable(listen) {
			return []net.Listener{v4}, port, nil
		}
		_ = v4.Close()
	}
	return nil, 0, errors.New("no loopback port free on both 127.0.0.1 and ::1")
}

// ipv6LoopbackAvailable reports whether [::1] can be listened on at all.
func ipv6LoopbackAvailable(listen listenFunc) bool {
	ln, err := listen("tcp", "[::1]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// serveWebSocket serves engine's `/ws/...` routes (and nothing else,
// router.WebSocketOnly) on lns until the returned stop function is called.
func serveWebSocket(lns []net.Listener, engine http.Handler) (stop func()) {
	srv := &http.Server{Handler: router.WebSocketOnly(engine), ReadHeaderTimeout: 10 * time.Second}
	for _, ln := range lns {
		go func() {
			if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				slog.Error("desktop: websocket listener stopped", "error", err, "addr", ln.Addr().String())
			}
		}()
	}
	return func() { _ = srv.Close() }
}

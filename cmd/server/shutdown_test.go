package main

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// shutdown must close an open WebSocket and wait for its handler to
// return: the handler runs on its request ctx, derived from the server
// context, and http.Server.Shutdown alone neither closes nor waits for
// hijacked connections.
func TestHTTPServer_ShutdownClosesWebSocketAndWaitsForHandler(t *testing.T) {
	var handlerReturned atomic.Bool
	accepted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		defer handlerReturned.Store(true)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		close(accepted)
		// Same shape as the real /ws handlers: run until the request ctx ends.
		<-r.Context().Done()
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
	})

	srv := newHTTPServer("", mux)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+ln.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	<-accepted
	// The close handshake needs the client reading, as a browser does.
	readErr := make(chan error, 1)
	go func() { _, _, err := conn.Read(ctx); readErr <- err }()

	if err := srv.shutdown(5 * time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !handlerReturned.Load() {
		t.Fatal("shutdown returned while the WebSocket handler was still running")
	}
	if err := <-readErr; websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("client Read err = %v, want a going-away close", err)
	}
}

// A handler that never returns must not hang shutdown past its timeout.
func TestHTTPServer_ShutdownGivesUpOnStuckHandler(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	srv := newHTTPServer("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	go func() { _, _ = http.Get("http://" + ln.Addr().String()) }() //nolint:noctx,bodyclose // test client
	<-started

	begin := time.Now()
	if err := srv.shutdown(200 * time.Millisecond); err == nil {
		t.Fatal("shutdown with a stuck handler: err = nil, want a timeout error")
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Fatalf("shutdown took %v with a 200ms timeout", d)
	}
}

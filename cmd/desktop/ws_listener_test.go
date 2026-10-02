package main

import (
	"errors"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func dialOK(t *testing.T, addr string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	_ = conn.Close()
}

func closeAll(lns []net.Listener) {
	for _, ln := range lns {
		_ = ln.Close()
	}
}

// Issue #285: *.localhost may resolve to ::1, so the WebSocket listener
// has to own the port on both loopback addresses.
func TestListenLoopback_BindsSamePortOnIPv4AndIPv6(t *testing.T) {
	if !ipv6LoopbackAvailable(net.Listen) {
		t.Skip("IPv6 loopback unavailable")
	}
	lns, port, err := listenLoopback(net.Listen, loopbackAttempts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll(lns)
	if len(lns) != 2 {
		t.Fatalf("got %d listeners, want 2 (127.0.0.1 and ::1)", len(lns))
	}
	dialOK(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	dialOK(t, net.JoinHostPort("::1", strconv.Itoa(port)))
}

// fakeListen wraps net.Listen so the test can make chosen addresses fail.
func fakeListen(fail func(address string) bool) listenFunc {
	return func(network, address string) (net.Listener, error) {
		if fail(address) {
			return nil, errors.New("fake: bind failed")
		}
		return net.Listen(network, address)
	}
}

func TestListenLoopback_RetriesWithNewPortWhenIPv6PortIsTaken(t *testing.T) {
	if !ipv6LoopbackAvailable(net.Listen) {
		t.Skip("IPv6 loopback unavailable")
	}
	var taken string
	listen := fakeListen(func(address string) bool {
		host, _, _ := net.SplitHostPort(address)
		if host != "::1" || address == "[::1]:0" {
			return false
		}
		if taken == "" { // the first concrete port is "in use" on ::1
			taken = address
			return true
		}
		return false
	})
	lns, port, err := listenLoopback(listen, loopbackAttempts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll(lns)
	if len(lns) != 2 || net.JoinHostPort("::1", strconv.Itoa(port)) == taken {
		t.Fatalf("got %d listeners on port %d (taken %s), want both addresses on a fresh port", len(lns), port, taken)
	}
	if taken == "" {
		t.Fatal("the busy-port path was never exercised")
	}
}

func TestListenLoopback_FallsBackToIPv4WithoutIPv6(t *testing.T) {
	listen := fakeListen(func(address string) bool {
		host, _, _ := net.SplitHostPort(address)
		return host == "::1"
	})
	lns, port, err := listenLoopback(listen, loopbackAttempts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll(lns)
	if len(lns) != 1 {
		t.Fatalf("got %d listeners, want only the IPv4 one", len(lns))
	}
	dialOK(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func TestListenLoopback_GivesUpWhenIPv6PortIsAlwaysTaken(t *testing.T) {
	if !ipv6LoopbackAvailable(net.Listen) {
		t.Skip("IPv6 loopback unavailable")
	}
	listen := fakeListen(func(address string) bool {
		host, _, _ := net.SplitHostPort(address)
		return host == "::1" && address != "[::1]:0"
	})
	lns, _, err := listenLoopback(listen, 3)
	if err == nil {
		closeAll(lns)
		t.Fatal("want an error when no port is free on both addresses")
	}
}

func TestListenWebSocket_OnlyOnWindows(t *testing.T) {
	lns, base := listenWebSocket()
	defer closeAll(lns)
	if runtime.GOOS != "windows" {
		if lns != nil || base != "" {
			t.Fatalf("listenWebSocket() = %v, %q on %s, want none", lns, base, runtime.GOOS)
		}
		return
	}
	if len(lns) == 0 || base != "ws://wails.localhost:"+strconv.Itoa(lns[0].Addr().(*net.TCPAddr).Port) {
		t.Fatalf("listenWebSocket() = %v, %q, want listeners and a wails.localhost base on their port", lns, base)
	}
}

// serveWebSocket must serve the engine's upgrades on every listener and
// nothing else, and stop serving when stopped.
func TestServeWebSocket_ServesUpgradesOnEveryListenerUntilStopped(t *testing.T) {
	lns, port, err := listenLoopback(net.Listen, loopbackAttempts)
	if err != nil {
		t.Fatal(err)
	}
	engine := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	stop := serveWebSocket(lns, engine)

	do := func(host, path, upgrade string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+path, nil)
		if upgrade != "" {
			req.Header.Set("Upgrade", upgrade)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", req.URL, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	hosts := []string{"127.0.0.1"}
	if len(lns) == 2 {
		hosts = append(hosts, "::1")
	}
	for _, host := range hosts {
		if got := do(host, "/ws/system", "websocket"); got != http.StatusAccepted {
			t.Errorf("%s upgrade of /ws/system = %d, want the engine's 202", host, got)
		}
		if got := do(host, "/scanner", "websocket"); got != http.StatusNotFound {
			t.Errorf("%s upgrade of /scanner = %d, want 404", host, got)
		}
		if got := do(host, "/ws/system", ""); got != http.StatusNotFound {
			t.Errorf("%s plain GET of /ws/system = %d, want 404", host, got)
		}
	}

	stop()
	for _, host := range hosts {
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second); err == nil {
			_ = conn.Close()
			t.Errorf("%s still accepts connections after stop", host)
		}
	}
}

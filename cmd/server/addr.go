package main

import (
	"fmt"
	"net"
)

// EnvAllowNonLoopback names the env var that must be set to "1" to let
// PITHA_SERVER_ADDR bind a non-loopback address. The HTTP server exposes
// Kill Switch, manual exits and secret writes to whoever can reach it
// (docs/api/endpoints.md §1 requires 127.0.0.1 only), so widening the bind
// is an explicit opt-in.
const EnvAllowNonLoopback = "PITHA_SERVER_ALLOW_NON_LOOPBACK"

// resolveListenAddr returns addr, or defaultAddr when it is empty, and
// rejects any address that would listen beyond loopback (an empty host such
// as ":48080", 0.0.0.0, a LAN IP or a hostname other than "localhost")
// unless allowNonLoopback is set.
func resolveListenAddr(addr string, allowNonLoopback bool) (string, error) {
	if addr == "" {
		return defaultAddr, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid PITHA_SERVER_ADDR %q: %w", addr, err)
	}
	if !allowNonLoopback && !isLoopbackHost(host) {
		return "", fmt.Errorf("PITHA_SERVER_ADDR %q is not a loopback address; use 127.0.0.1/[::1]/localhost, or set %s=1 to expose the server deliberately", addr, EnvAllowNonLoopback)
	}
	return addr, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

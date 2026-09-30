package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// EnvAllowNonLoopback names the env var that must be set to "1" to let
// PITHA_SERVER_ADDR bind a non-loopback address. The HTTP server exposes
// Kill Switch, manual exits and secret writes to whoever can reach it
// (docs/api/endpoints.md §1 requires 127.0.0.1 only), so widening the bind
// is an explicit opt-in.
const EnvAllowNonLoopback = "PITHA_SERVER_ALLOW_NON_LOOPBACK"

// EnvAllowedHosts names the env var listing extra Host header names
// (comma-separated hostnames, e.g. a LAN name) the server accepts. It only
// applies together with EnvAllowNonLoopback=1.
const EnvAllowedHosts = "PITHA_SERVER_ALLOWED_HOSTS"

// allowedHosts returns the Host header names cmd/server accepts (DNS
// rebinding defence, issue #136): the loopback names always, plus, when
// allowNonLoopback is set, the bind host of addr (unless it is a wildcard)
// and the comma-separated extraHosts.
func allowedHosts(addr string, allowNonLoopback bool, extraHosts string) []string {
	hosts := middleware.LoopbackHosts()
	if !allowNonLoopback {
		return hosts
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
			hosts = append(hosts, host)
		}
	}
	for _, extra := range strings.Split(extraHosts, ",") {
		if extra = strings.TrimSpace(extra); extra != "" {
			hosts = append(hosts, extra)
		}
	}
	return hosts
}

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

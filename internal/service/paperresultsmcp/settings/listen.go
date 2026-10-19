package settings

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// tailnetPrefixes are the address ranges Tailscale assigns to a node's
// tailnet interface: the IPv4 CGNAT block and the IPv6 ULA block.
var tailnetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// InterfaceAddrs lists the addresses assigned to this host's interfaces
// (net.InterfaceAddrs in production; a fake in tests).
type InterfaceAddrs func() ([]net.Addr, error)

// ErrListenNotAllowed is returned for a listen address outside architecture
// §3.1: only a loopback address, or this PC's own tailnet address, is allowed.
var ErrListenNotAllowed = errors.New("paper-results-mcp: listen address is not allowed (loopback or this PC's tailnet address only)")

// ErrTailnetNotAssigned is wrapped (together with ErrListenNotAllowed) when
// the address is a tailnet address that no local interface has right now.
// Unlike every other refusal it can clear by itself: Tailscale may not have
// brought its interface up yet just after the PC logs on.
var ErrTailnetNotAssigned = errors.New("paper-results-mcp: tailnet address is not assigned to a local interface")

// ValidateListen reports whether addr ("host:port") may be bound. The host
// must be an IP literal - a name could resolve to a LAN or public address -
// and either loopback, or a tailnet address (100.64.0.0/10,
// fd7a:115c:a1e0::/48) that is currently assigned to a local interface; it
// never falls back to another interface. 0.0.0.0, ::, LAN and any other
// address are refused, and so is port 0.
func ValidateListen(addr string, ifaceAddrs InterfaceAddrs) error {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: not host:port", ErrListenNotAllowed)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%w: invalid port", ErrListenNotAllowed)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return fmt.Errorf("%w: host must be an IP literal", ErrListenNotAllowed)
	}
	ip = ip.Unmap()
	switch {
	case ip.IsUnspecified():
		return fmt.Errorf("%w: wildcard address", ErrListenNotAllowed)
	case ip.IsLoopback():
		return nil
	case !isTailnet(ip):
		return ErrListenNotAllowed
	}
	return requireLocalAddress(ip, ifaceAddrs)
}

func isTailnet(ip netip.Addr) bool {
	for _, p := range tailnetPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func requireLocalAddress(ip netip.Addr, ifaceAddrs InterfaceAddrs) error {
	if ifaceAddrs == nil {
		ifaceAddrs = net.InterfaceAddrs
	}
	addrs, err := ifaceAddrs()
	if err != nil {
		return fmt.Errorf("paper-results-mcp: list interface addresses: %w", err)
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if local, ok := netip.AddrFromSlice(ipNet.IP); ok && local.Unmap() == ip {
			return nil
		}
	}
	return fmt.Errorf("%w: %w", ErrListenNotAllowed, ErrTailnetNotAssigned)
}

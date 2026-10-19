package settings

import (
	"errors"
	"net"
	"testing"
)

func fakeIfaces(addrs ...string) InterfaceAddrs {
	return func() ([]net.Addr, error) {
		out := make([]net.Addr, 0, len(addrs))
		for _, a := range addrs {
			ip, ipNet, err := net.ParseCIDR(a)
			if err != nil {
				return nil, err
			}
			ipNet.IP = ip
			out = append(out, ipNet)
		}
		return out, nil
	}
}

// 127.0.0.1:48110 is accepted; 0.0.0.0, ::, LAN and public addresses,
// hostnames and bad ports are rejected (architecture §3.1).
func TestValidateListen(t *testing.T) {
	tailnet := fakeIfaces("127.0.0.1/8", "192.168.1.20/24", "100.101.102.103/32", "fd7a:115c:a1e0::1234/128")

	accepted := []string{
		"127.0.0.1:48110",
		"127.0.0.1:1",
		"[::1]:48110",
		"100.101.102.103:48110", // this PC's own tailnet address
		"[fd7a:115c:a1e0::1234]:48110",
	}
	for _, addr := range accepted {
		if err := ValidateListen(addr, tailnet); err != nil {
			t.Errorf("ValidateListen(%q) = %v, want nil", addr, err)
		}
	}

	rejected := []string{
		"0.0.0.0:48110",
		"[::]:48110",
		":48110",             // empty host = all interfaces
		"192.168.1.20:48110", // LAN address that is assigned locally
		"10.0.0.5:48110",     // private, not assigned
		"8.8.8.8:48110",      // public
		"100.64.0.9:48110",   // tailnet range but not this PC's address: no fallback
		"[fd7a:115c:a1e0::9]:48110",
		"localhost:48110", // names can resolve elsewhere
		"example.com:48110",
		"127.0.0.1:0",
		"127.0.0.1:65536",
		"127.0.0.1:port",
		"127.0.0.1",
		"[::ffff:0.0.0.0]:48110",
		"[fe80::1%eth0]:48110",
		"",
	}
	for _, addr := range rejected {
		err := ValidateListen(addr, tailnet)
		if !errors.Is(err, ErrListenNotAllowed) {
			t.Errorf("ValidateListen(%q) = %v, want ErrListenNotAllowed", addr, err)
		}
	}
}

// A tailnet address with no matching interface (Tailscale down) is a
// refusal, never a different interface.
func TestValidateListenTailnetWithoutInterface(t *testing.T) {
	noTailnet := fakeIfaces("127.0.0.1/8", "192.168.1.20/24")
	if err := ValidateListen("100.101.102.103:48110", noTailnet); !errors.Is(err, ErrListenNotAllowed) {
		t.Fatalf("err = %v, want ErrListenNotAllowed", err)
	}
	failing := func() ([]net.Addr, error) { return nil, errors.New("boom") }
	if err := ValidateListen("100.101.102.103:48110", failing); err == nil {
		t.Fatal("interface listing failure must refuse to listen")
	}
	// Loopback needs no interface lookup at all.
	if err := ValidateListen("127.0.0.1:48110", failing); err != nil {
		t.Fatalf("loopback must not depend on interface listing: %v", err)
	}
}

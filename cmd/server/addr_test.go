package main

import (
	"net"
	"testing"
)

func TestDefaultAddrIsLoopback(t *testing.T) {
	got, err := resolveListenAddr("", false)
	if err != nil || got != defaultAddr {
		t.Fatalf("resolveListenAddr(\"\") = %q, %v; want %q", got, err, defaultAddr)
	}
	host, _, err := net.SplitHostPort(defaultAddr)
	if err != nil || !isLoopbackHost(host) {
		t.Fatalf("defaultAddr %q must bind loopback only (host=%q, err=%v)", defaultAddr, host, err)
	}
}

func TestResolveListenAddr(t *testing.T) {
	for _, tc := range []struct {
		addr       string
		allowNonLB bool
		wantErr    bool
	}{
		{"127.0.0.1:9000", false, false},
		{"localhost:9000", false, false},
		{"[::1]:9000", false, false},
		{":48080", false, true},
		{"0.0.0.0:48080", false, true},
		{"[::]:48080", false, true},
		{"192.168.1.10:48080", false, true},
		{"example.com:48080", false, true},
		{"no-port", false, true},
		{":48080", true, false},
		{"0.0.0.0:48080", true, false},
		{"no-port", true, true},
	} {
		got, err := resolveListenAddr(tc.addr, tc.allowNonLB)
		if (err != nil) != tc.wantErr {
			t.Errorf("resolveListenAddr(%q, %v) err = %v, wantErr %v", tc.addr, tc.allowNonLB, err, tc.wantErr)
		}
		if err == nil && got != tc.addr {
			t.Errorf("resolveListenAddr(%q, %v) = %q, want unchanged", tc.addr, tc.allowNonLB, got)
		}
	}
}

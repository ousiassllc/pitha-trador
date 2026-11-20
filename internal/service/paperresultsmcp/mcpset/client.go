package mcpset

import "net"

// ClientHost is the caller's host for the audit log: the IP of the TCP peer
// (the request's RemoteAddr), never a header value (headers are caller-controlled).
func ClientHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

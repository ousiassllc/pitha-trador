package tachibana

import "context"

// Hooks for the external tests (tachibana_test).
var (
	DecryptVirtualURL   = decryptVirtualURL
	CountsAsFeedFailure = countsAsFeedFailure
	FormatSDDate        = formatSDDate
)

// NewVirtualURLs builds the URL holder (unexported fields).
func NewVirtualURLs(request, eventWS string) any {
	return virtualURLs{request: request, eventWebSocket: eventWS}
}

// Gate exposes the priority gate.
type Gate struct{ g gate }

// Acquire takes the gate.
func (g *Gate) Acquire(ctx context.Context, p Priority) (func(), error) { return g.g.acquire(ctx, p) }

// Waiting is how many acquirers are queued.
func (g *Gate) Waiting() int {
	g.g.mu.Lock()
	defer g.g.mu.Unlock()
	return len(g.g.waiters)
}

// SessionURLs returns the current virtual URLs (for redaction tests).
func (c *Client) SessionURLs() any {
	urls, _, _ := c.session()
	return urls
}

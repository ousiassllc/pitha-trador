package tachibana

import (
	"context"
	"strings"
)

// EventSession is the EVENT I/F (WebSocket) access of the current login. URL
// is a bearer credential: pass it to the dialer and never log or format it
// (errors from the dialer go through Client.Sanitize).
type EventSession struct {
	// URL is the decrypted sUrlEventWebSocket, without query.
	URL string
	// Gen identifies the login; ReportSessionLost takes it back.
	Gen uint64
}

// String redacts the URL.
func (EventSession) String() string { return redacted }

// GoString redacts the URL.
func (EventSession) GoString() string { return redacted }

// EventSession returns the EVENT-WebSocket virtual URL of the current login,
// false when there is no session (not logged in yet, lost, or past the
// 03:30 close).
func (c *Client) EventSession() (EventSession, bool) {
	urls, gen, ok := c.session()
	if !ok || urls.eventWebSocket == "" {
		return EventSession{}, false
	}
	return EventSession{URL: urls.eventWebSocket, Gen: gen}, true
}

// SessionChanged returns a channel that is closed when the next login
// installs a new session. Take it before calling EventSession so that a login
// in between is not missed.
func (c *Client) SessionChanged() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changed
}

// ReportSessionLost tells the Client (and through it the session manager)
// that the EVENT connection learned the session behind gen is gone
// (ST with p_errno=2): the virtual URL is dropped and a re-login follows.
func (c *Client) ReportSessionLost(ctx context.Context, gen uint64) {
	c.afterSession(ctx, gen, &APIError{Errno: ErrnoSessionExpired, Text: "session inactive"})
}

// Sanitize returns err without any virtual URL: the URL of a *url.Error is
// cut off and every remaining occurrence of one of the session's URLs in the
// message is replaced. The cause stays reachable through errors.Is/As.
func (c *Client) Sanitize(err error) error {
	if err == nil {
		return nil
	}
	err = transportError(err)
	c.mu.Lock()
	urls := c.urls
	c.mu.Unlock()
	msg := err.Error()
	clean := msg
	for _, u := range []string{urls.request, urls.master, urls.price, urls.event, urls.eventWebSocket} {
		if u == "" {
			continue
		}
		clean = strings.ReplaceAll(clean, u, redacted)
		if _, rest, found := strings.Cut(u, "://"); found {
			clean = strings.ReplaceAll(clean, rest, redacted)
		}
	}
	if clean == msg {
		return err
	}
	return &sanitizedError{msg: clean, cause: err}
}

type sanitizedError struct {
	msg   string
	cause error
}

func (e *sanitizedError) Error() string { return e.msg }

func (e *sanitizedError) Unwrap() error { return e.cause }

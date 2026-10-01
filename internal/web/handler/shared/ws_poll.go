package shared

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// WriteJSON marshals v and sends it to conn as a text frame.
func WriteJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// MaxConsecutiveTransientErrors is how many consecutive Transient step
// failures PollWebSocket tolerates before it gives up and closes the
// connection. A failure that persists (e.g. a missing dependency) must not
// leave the page looking connected while it never receives an update: the
// close makes the browser show the disconnect notice, which is then true.
const MaxConsecutiveTransientErrors = 5

// transientError marks a step failure that must not end the connection.
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

// Transient marks err, a failure to read the data a PollWebSocket step
// pushes, as possibly recoverable: PollWebSocket logs it and retries on the
// next poll instead of closing the connection, up to
// MaxConsecutiveTransientErrors in a row. Closing on the first failure
// makes the browser report a lost connection and reconnect although the
// connection itself is fine (issue #266). Errors that are not marked (a
// failed write: the peer is gone; an unknown symbol: it will never recover)
// still end the connection at once. Transient(nil) is nil.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return transientError{err: err}
}

// PollWebSocket accepts a WebSocket upgrade on c and calls step once
// immediately, then again after each next() delay, until step returns an
// error that is not Transient, MaxConsecutiveTransientErrors Transient
// errors occur in a row, or the client goes away. Only the first failure of
// a run is logged (Warn) so a persistent one does not flood the log; the
// recovery is logged at Info.
//
// The clients of these endpoints never send, so the connection is put in
// CloseRead mode: control frames (Ping/Close) are handled and the ctx
// passed to step is cancelled as soon as the client disconnects, instead
// of the handler lingering until the next write fails (up to a full
// polling interval).
func PollWebSocket(c *gin.Context, next func() time.Duration, step func(ctx context.Context, conn *websocket.Conn) error) {
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx := conn.CloseRead(c.Request.Context())
	path := c.Request.URL.Path
	failures := 0

	for {
		if err := step(ctx, conn); err != nil {
			var transient transientError
			if !errors.As(err, &transient) || ctx.Err() != nil {
				return
			}
			failures++
			if failures >= MaxConsecutiveTransientErrors {
				slog.Error("ws: poll step kept failing, closing connection", "path", path, "failures", failures, "error", err)
				_ = conn.Close(websocket.StatusInternalError, "data unavailable")
				return
			}
			if failures == 1 {
				slog.Warn("ws: poll step failed, keeping connection open", "path", path, "error", err)
			}
		} else if failures > 0 {
			slog.Info("ws: poll step recovered", "path", path, "failures", failures)
			failures = 0
		}

		timer := time.NewTimer(next())
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-timer.C:
		}
	}
}

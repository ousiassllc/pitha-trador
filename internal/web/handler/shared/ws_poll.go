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

// transientError marks a step failure that must not end the connection.
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

// Transient marks err, a failure to read the data a PollWebSocket step
// pushes (e.g. a momentarily busy database), as recoverable: PollWebSocket
// logs it and retries on the next poll instead of closing the connection.
// Closing would make the browser report a lost connection and reconnect
// even though the connection itself is fine (issue #266). Errors that are
// not marked (a failed write: the peer is gone; an unknown symbol: it will
// never recover) still end the connection. Transient(nil) is nil.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return transientError{err: err}
}

// PollWebSocket accepts a WebSocket upgrade on c and calls step once
// immediately, then again after each next() delay, until step returns an
// error that is not Transient or the client goes away. A Transient error
// is logged and the connection is kept open.
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

	for {
		if err := step(ctx, conn); err != nil {
			var transient transientError
			if !errors.As(err, &transient) || ctx.Err() != nil {
				return
			}
			slog.Warn("ws: poll step failed, keeping connection open", "path", c.Request.URL.Path, "error", err)
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

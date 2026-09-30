package shared

import (
	"context"
	"encoding/json"
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

// PollWebSocket accepts a WebSocket upgrade on c and calls step once
// immediately, then again after each next() delay, until step returns an
// error or the client goes away.
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
			return
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

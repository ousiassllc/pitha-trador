package shared_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// TestPollWebSocket_ClientCloseEndsHandlerPromptly guards issue #127: the
// polling WebSocket handlers must end as soon as the client closes, rather
// than waiting out their (here: one hour) polling interval for the next
// write to fail.
func TestPollWebSocket_ClientCloseEndsHandlerPromptly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const longInterval = time.Hour

	returned := make(chan struct{})
	engine := gin.New()
	engine.GET("/ws", func(c *gin.Context) {
		defer close(returned)
		shared.PollWebSocket(c, func() time.Duration { return longInterval },
			func(ctx context.Context, conn *websocket.Conn) error {
				return shared.WriteJSON(ctx, conn, map[string]string{"type": "tick"})
			})
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The first step's message proves the handler is now idling in its
	// interval wait (CloseRead is what lets the server see the Close frame
	// at all).
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("conn.Read() error = %v", err)
	}
	go func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	// Drain so the client side can finish the close handshake.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatalf("handler still running 3s after client close (interval %v)", longInterval)
	}
}

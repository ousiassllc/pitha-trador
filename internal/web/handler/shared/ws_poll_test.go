package shared_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

func dialPoll(t *testing.T, step func(ctx context.Context, conn *websocket.Conn) error) (context.Context, *websocket.Conn) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/ws", func(c *gin.Context) {
		shared.PollWebSocket(c, func() time.Duration { return 10 * time.Millisecond }, step)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return ctx, conn
}

// Issue #266: a failed data read (e.g. a momentarily busy DB) used to end
// the handler, so the browser saw its WebSocket drop and showed "接続が切れて
// います" although nothing was wrong with the connection. A transient step
// error must leave the connection open and be retried on the next poll.
func TestPollWebSocket_TransientStepErrorKeepsConnectionOpen(t *testing.T) {
	var calls atomic.Int64
	ctx, conn := dialPoll(t, func(ctx context.Context, conn *websocket.Conn) error {
		if calls.Add(1) <= 2 {
			return shared.Transient(errors.New("database is locked"))
		}
		return shared.WriteJSON(ctx, conn, map[string]string{"type": "ok"})
	})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v, want the message pushed after the transient errors", err)
	}
	if !strings.Contains(string(data), `"ok"`) {
		t.Fatalf("message = %s, want the ok message", data)
	}
}

// Any error that is not marked transient (e.g. a failed write: the peer is
// gone) still ends the connection.
func TestPollWebSocket_PlainStepErrorClosesConnection(t *testing.T) {
	ctx, conn := dialPoll(t, func(context.Context, *websocket.Conn) error {
		return errors.New("fatal")
	})

	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("conn.Read() error = %v (ctx err %v), want the server to close the connection", err, ctx.Err())
	}
}

// A Transient error that never clears must not keep the connection open
// forever with no updates: after MaxConsecutiveTransientErrors it closes.
func TestPollWebSocket_PersistentTransientErrorClosesConnection(t *testing.T) {
	var calls atomic.Int64
	ctx, conn := dialPoll(t, func(context.Context, *websocket.Conn) error {
		calls.Add(1)
		return shared.Transient(errors.New("still broken"))
	})

	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("conn.Read() error = %v (ctx err %v), want the server to give up and close", err, ctx.Err())
	}
	if got := calls.Load(); got != shared.MaxConsecutiveTransientErrors {
		t.Fatalf("step calls = %d, want %d", got, shared.MaxConsecutiveTransientErrors)
	}
}

// Failures only count while consecutive: a success resets the budget.
func TestPollWebSocket_SuccessResetsTransientErrorBudget(t *testing.T) {
	var calls atomic.Int64
	ctx, conn := dialPoll(t, func(ctx context.Context, conn *websocket.Conn) error {
		// Fail MaxConsecutiveTransientErrors-1 times, succeed, repeat: never
		// reaches the limit, so every success message must arrive.
		n := calls.Add(1)
		if n%shared.MaxConsecutiveTransientErrors != 0 {
			return shared.Transient(errors.New("flaky"))
		}
		return shared.WriteJSON(ctx, conn, map[string]int64{"n": n})
	})

	for i := 0; i < 3; i++ {
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("conn.Read() #%d error = %v, want the connection to stay open", i, err)
		}
	}
}

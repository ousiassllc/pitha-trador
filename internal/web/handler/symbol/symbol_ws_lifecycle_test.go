package symbol_test

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

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// TestWebSocket_ClientCloseEndsHandlerPromptly guards issue #127: the
// handler must end as soon as the client closes, rather than waiting out its
// (here: one hour) polling interval for the next write to fail.
func TestWebSocket_ClientCloseEndsHandlerPromptly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const longInterval = time.Hour

	h := symbol.NewSymbolHandler(
		&fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastPrice: 1, LastScanAt: timePtr(time.Now())}},
		symbol.SymbolRiskParams{},
	)
	h.SetTickInterval(longInterval)

	returned := make(chan struct{})
	engine := gin.New()
	engine.GET("/ws/symbols/:symbol", func(c *gin.Context) {
		defer close(returned)
		h.WebSocket(c)
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/symbols/7203"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Complete the handshake with the handler idling in its interval wait
	// before closing (CloseRead is what lets the server see the Close frame
	// at all).
	time.Sleep(50 * time.Millisecond)
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

// failingFirstSymbolProvider fails its first failures State calls with err.
type failingFirstSymbolProvider struct {
	*fakeSymbolProvider
	failures int
	err      error
	calls    atomic.Int64
}

func (p *failingFirstSymbolProvider) State(ctx context.Context, symbol string) (execution.SymbolState, error) {
	if int(p.calls.Add(1)) <= p.failures {
		return execution.SymbolState{}, p.err
	}
	return p.fakeSymbolProvider.State(ctx, symbol)
}

func dialSymbolWS(t *testing.T, provider symbol.SymbolProvider) (context.Context, *websocket.Conn) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	h.SetTickInterval(10 * time.Millisecond)
	engine := gin.New()
	engine.GET("/ws/symbols/:symbol", h.WebSocket)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/symbols/7203", nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return ctx, conn
}

// Issue #266: a failed state read must not drop the connection (the browser
// would show "接続が切れています" although the connection is fine).
func TestSymbolHandler_WebSocket_StateErrorKeepsConnectionOpen(t *testing.T) {
	provider := &failingFirstSymbolProvider{
		fakeSymbolProvider: &fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastPrice: 100, LastScanAt: timePtr(time.Now())}},
		failures:           2,
		err:                errors.New("database is locked"),
	}
	ctx, conn := dialSymbolWS(t, provider)

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v, want the tick pushed after the transient errors", err)
	}
	if !strings.Contains(string(data), `"tick"`) {
		t.Fatalf("message = %s, want a tick", data)
	}
}

// An unknown symbol never recovers, so the connection ends at once.
func TestSymbolHandler_WebSocket_UnknownSymbolClosesConnection(t *testing.T) {
	provider := &failingFirstSymbolProvider{
		fakeSymbolProvider: &fakeSymbolProvider{},
		failures:           1 << 30,
		err:                execution.ErrInstrumentUnknown,
	}
	ctx, conn := dialSymbolWS(t, provider)

	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("conn.Read() error = %v (ctx err %v), want the server to close the connection", err, ctx.Err())
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("State calls = %d, want 1 (no retry for an unknown symbol)", got)
	}
}

// A persistent failure (e.g. a missing dependency) must not leave the page
// looking connected forever: after shared.MaxConsecutiveTransientErrors the
// connection is closed.
func TestSymbolHandler_WebSocket_PersistentStateErrorClosesConnection(t *testing.T) {
	provider := &failingFirstSymbolProvider{
		fakeSymbolProvider: &fakeSymbolProvider{},
		failures:           1 << 30,
		err:                errors.New("execution: State requires Deps"),
	}
	ctx, conn := dialSymbolWS(t, provider)

	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("conn.Read() error = %v (ctx err %v), want the server to give up and close", err, ctx.Err())
	}
	if got := provider.calls.Load(); got != shared.MaxConsecutiveTransientErrors {
		t.Fatalf("State calls = %d, want %d", got, shared.MaxConsecutiveTransientErrors)
	}
}

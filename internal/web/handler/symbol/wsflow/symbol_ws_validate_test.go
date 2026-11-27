package wsflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// issue #1231: WebSocket validates the symbol format (same as Page and the
// JSON API) and answers 404 before upgrading, instead of completing the
// handshake (101) and closing afterwards.
func TestSymbolHandler_WebSocket_InvalidSymbolFormatReturns404WithoutUpgrade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{state: execution.SymbolState{Symbol: "x", LastPrice: 1}}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	h.SetTickInterval(20 * time.Millisecond)
	engine := gin.New()
	engine.GET("/ws/symbols/:symbol", h.WebSocket)
	server := httptest.NewServer(engine)
	defer server.Close()

	for name, path := range map[string]string{
		"dot":       "a.b",
		"percent":   "a%25b",
		"too long":  "12345678901234567",
		"non-ascii": "%E3%83%88%E3%83%A8%E3%82%BF",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/symbols/" + path
			conn, resp, err := websocket.Dial(ctx, wsURL, nil)
			if err == nil {
				_ = conn.CloseNow()
				t.Fatalf("websocket.Dial() succeeded, want handshake failure")
			}
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				t.Fatalf("handshake response = %v, want status %d", resp, http.StatusNotFound)
			}
		})
	}
}

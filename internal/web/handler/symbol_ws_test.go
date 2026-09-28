package handler_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestSymbolHandler_WebSocket_PushesTickAndJevUpdateMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	confidence := 0.74
	provider := &fakeSymbolProvider{state: execution.SymbolState{
		Symbol: "7203", LastPrice: 2105.5, LastSignal: domain.JevDirectionLong, LastSignalConfidence: confidence,
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	h.SetTickInterval(20 * time.Millisecond)

	engine := gin.New()
	engine.GET("/ws/symbols/:symbol", h.WebSocket)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/symbols/7203"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The first tick and its jev_update (direction just became known)
	// both arrive immediately on connect, before the first interval
	// tick.
	var tick struct {
		Type  string  `json:"type"`
		Price float64 `json:"price"`
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() [tick] error = %v", err)
	}
	if err := json.Unmarshal(data, &tick); err != nil {
		t.Fatalf("json.Unmarshal() [tick] error = %v, data = %s", err, data)
	}
	if tick.Type != "tick" || tick.Price != 2105.5 {
		t.Fatalf("tick = %+v, want Type=tick Price=2105.5", tick)
	}

	var jevUpdate struct {
		Type       string   `json:"type"`
		Direction  *string  `json:"direction"`
		Confidence *float64 `json:"confidence"`
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() [jev_update] error = %v", err)
	}
	if err := json.Unmarshal(data, &jevUpdate); err != nil {
		t.Fatalf("json.Unmarshal() [jev_update] error = %v, data = %s", err, data)
	}
	if jevUpdate.Type != "jev_update" || jevUpdate.Direction == nil || *jevUpdate.Direction != domain.JevDirectionLong {
		t.Fatalf("jev_update = %+v, want Type=jev_update Direction=LONG", jevUpdate)
	}
	if jevUpdate.Confidence == nil || *jevUpdate.Confidence != confidence {
		t.Fatalf("jev_update.Confidence = %v, want %v", jevUpdate.Confidence, confidence)
	}

	// A second tick follows within the 20ms interval, proving the
	// handler loops rather than pushing once and stopping. No second
	// jev_update is expected: direction/confidence have not changed.
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() [second tick] error = %v", err)
	}
	if err := json.Unmarshal(data, &tick); err != nil {
		t.Fatalf("json.Unmarshal() [second tick] error = %v, data = %s", err, data)
	}
	if tick.Type != "tick" {
		t.Fatalf("second message.Type = %q, want %q (no jev_update since nothing changed)", tick.Type, "tick")
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestSymbolHandler_WebSocket_NoJevUpdateWhenNoSignalYet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{state: execution.SymbolState{
		Symbol: "7203", LastPrice: 2100.0, LastSignal: domain.JevDirectionNone,
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	h.SetTickInterval(200 * time.Millisecond)

	engine := gin.New()
	engine.GET("/ws/symbols/:symbol", h.WebSocket)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/symbols/7203"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var got struct {
		Type string `json:"type"`
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, data = %s", err, data)
	}
	if got.Type != "tick" {
		t.Fatalf("first message.Type = %q, want %q (no jev_update: LastSignal is NONE)", got.Type, "tick")
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

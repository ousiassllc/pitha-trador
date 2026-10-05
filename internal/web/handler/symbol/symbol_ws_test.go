package symbol_test

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
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

func TestSymbolHandler_WebSocket_PushesTickAndJevUpdateMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	confidence := 0.74
	scanAt := time.Date(2026, 10, 5, 9, 30, 15, 0, time.UTC)
	provider := &fakeSymbolProvider{
		state: stateWithTrader(execution.SymbolState{Symbol: "7203", LastPrice: 2105.5, LastScanAt: &scanAt, LastSignal: domain.JevDirectionShort, LastSignalConfidence: 0.1},
			traderDecision(domain.JevDirectionLong, confidence)),
	}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
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
	// both arrive immediately on connect, before the first interval tick.
	var tick struct {
		Type  string    `json:"type"`
		Price float64   `json:"price"`
		Time  time.Time `json:"time"`
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
	if !tick.Time.Equal(scanAt) {
		t.Fatalf("tick.Time = %v, want the snapshot time %v", tick.Time, scanAt)
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

	// The snapshot has not advanced, so no further tick (and no
	// jev_update: direction/confidence have not changed) follows.
	quietCtx, quietCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer quietCancel()
	if _, data, err = conn.Read(quietCtx); err == nil {
		t.Fatalf("unexpected message %s: the snapshot did not change", data)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestSymbolHandler_WebSocket_NoJevUpdateWhenNoTraderDecisionYet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A Policy signal alone must not produce a jev_update.
	provider := &fakeSymbolProvider{state: execution.SymbolState{
		Symbol: "7203", LastPrice: 2100.0, LastScanAt: timePtr(time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)), LastSignal: domain.JevDirectionLong, LastSignalConfidence: 0.8,
	}}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
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
		t.Fatalf("first message.Type = %q, want %q (no jev_update: no Trader decision yet)", got.Type, "tick")
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// A symbol without any snapshot has LastPrice 0; sending tick{price:0} made
// pitha-price-chart autoscale down to 0 (issue #183).
func TestSymbolHandler_WebSocket_SkipsTickWhileNoPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{
		state: stateWithTrader(execution.SymbolState{Symbol: "7203", LastPrice: 0}, traderDecision(domain.JevDirectionLong, 0.5)),
	}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	h.SetTickInterval(20 * time.Millisecond)

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

	// Several intervals elapse; the only message is the jev_update.
	readCtx, readCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer readCancel()
	var types []string
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		var got struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("json.Unmarshal() error = %v, data = %s", err, data)
		}
		types = append(types, got.Type)
	}
	if len(types) != 1 || types[0] != "jev_update" {
		t.Fatalf("messages = %v, want only [jev_update] (no tick while LastPrice is 0)", types)
	}
}

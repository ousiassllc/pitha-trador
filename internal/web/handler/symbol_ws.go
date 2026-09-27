package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// symbolTickMessage mirrors docs/api/endpoints.md §6's
// `{"type":"tick","price":2831.5,...}` message.
type symbolTickMessage struct {
	Type  string  `json:"type"`
	Price float64 `json:"price"`
}

// symbolJevUpdateMessage mirrors the same section's
// `{"type":"jev_update","direction":"LONG",...}` message.
type symbolJevUpdateMessage struct {
	Type       string   `json:"type"`
	Direction  *string  `json:"direction"`
	Confidence *float64 `json:"confidence"`
}

// WebSocket implements `/ws/symbols/{symbol}` (docs/api/endpoints.md §6):
// pushes a `tick` message every h.tickInterval, plus a `jev_update`
// message whenever the latest Jev Trader direction/confidence changes
// from what was last pushed - `pitha-price-chart`'s live update/marker
// source.
func (h *SymbolHandler) WebSocket(c *gin.Context) {
	symbol := c.Param("symbol")
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx := c.Request.Context()
	var lastDirection *string
	var lastConfidence *float64

	for {
		state, err := h.provider.State(ctx, symbol)
		if err != nil {
			return
		}

		if err := writeJSON(ctx, conn, symbolTickMessage{Type: "tick", Price: state.LastPrice}); err != nil {
			return
		}

		direction := jevDirectionOrNil(state.LastSignal)
		confidence := state.LastSignalConfidence
		changed := directionChanged(lastDirection, direction) || lastConfidence == nil || *lastConfidence != confidence
		if direction != nil && changed {
			msg := symbolJevUpdateMessage{Type: "jev_update", Direction: direction, Confidence: &confidence}
			if err := writeJSON(ctx, conn, msg); err != nil {
				return
			}
			lastDirection, lastConfidence = direction, &confidence
		}

		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-time.After(h.tickInterval):
		}
	}
}

// jevDirectionOrNil returns nil for domain.JevDirectionNone (no signal
// yet) so symbolJevUpdateMessage.Direction matches the "not evaluated
// yet" shape other Jev-derived JSON fields use elsewhere in this package.
func jevDirectionOrNil(direction string) *string {
	if direction == domain.JevDirectionNone {
		return nil
	}
	return &direction
}

func directionChanged(prev, next *string) bool {
	if prev == nil || next == nil {
		return prev != next
	}
	return *prev != *next
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

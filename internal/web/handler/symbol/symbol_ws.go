package symbol

import (
	"context"
	"errors"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
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
	var lastDirection *string
	var lastConfidence *float64

	shared.PollWebSocket(c, func() time.Duration { return h.tickInterval }, func(ctx context.Context, conn *websocket.Conn) error {
		state, err := h.provider.State(ctx, symbol)
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return err // never recovers: end the connection
		}
		if err != nil {
			return shared.Transient(err)
		}

		decision, err := h.latestTraderDecision(ctx, symbol)
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return err
		}
		if err != nil {
			return shared.Transient(err)
		}

		// No snapshot yet (LastPrice == 0): a price-0 tick would drag the
		// chart's autoscale to 0, so send nothing until a price exists.
		if state.LastPrice > 0 {
			if err := shared.WriteJSON(ctx, conn, symbolTickMessage{Type: "tick", Price: state.LastPrice}); err != nil {
				return err
			}
		}

		// No Trader decision yet, or one without a usable direction: nothing
		// to push (a null direction is never a jev_update).
		var direction *string
		var confidence *float64
		if decision != nil {
			direction, confidence = jevDirectionOrNil(decision.Direction), decision.Confidence
		}
		changed := directionChanged(lastDirection, direction) || !floatPtrEqual(lastConfidence, confidence)
		if direction != nil && changed {
			msg := symbolJevUpdateMessage{Type: "jev_update", Direction: direction, Confidence: confidence}
			if err := shared.WriteJSON(ctx, conn, msg); err != nil {
				return err
			}
			lastDirection, lastConfidence = direction, confidence
		}
		return nil
	})
}

// jevDirectionOrNil returns nil for a missing or domain.JevDirectionNone
// direction (no Trader call yet / no stance) so
// symbolJevUpdateMessage.Direction matches the "not evaluated yet" shape
// other Jev-derived JSON fields use elsewhere in this package.
func jevDirectionOrNil(direction *string) *string {
	if direction == nil || *direction == domain.JevDirectionNone {
		return nil
	}
	return direction
}

func directionChanged(prev, next *string) bool {
	if prev == nil || next == nil {
		return prev != next
	}
	return *prev != *next
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

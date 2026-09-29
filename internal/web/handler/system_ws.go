package handler

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// systemKillSwitchMessage mirrors docs/api/endpoints.md §6's
// `{"type":"kill_switch","reason":"daily_loss_limit"}` message.
type systemKillSwitchMessage struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// defaultManualKillReason is pushed when the system is Killed but has no
// active kill_switch_events row - a manual `POST /system/kill`
// (FR-RISK-4) is tracked only via the `system.killed` runtime_settings
// flag, not a kill_switch_events row (domain.KillSwitchEvent's doc
// comment).
const defaultManualKillReason = "manual"

// WebSocket implements `/ws/system` (docs/api/endpoints.md §6,
// components/overview.md §5.4): pushes a `kill_switch` message the
// moment SystemEngine.State transitions into domain.SystemStateKilled -
// whether triggered by `POST /system/kill` or directly by Risk Engine
// (architecture/overview.md §8.3) - so `pitha-kill-switch-panel` can
// reflect that state even when the transition did not originate from its
// own POST call.
func (h *SystemHandler) WebSocket(c *gin.Context) {
	wasKilled := false
	pollWebSocket(c, func() time.Duration { return h.pollInterval }, func(ctx context.Context, conn *websocket.Conn) error {
		state, events, err := h.engine.State(ctx)
		if err != nil {
			return err
		}

		if state == domain.SystemStateKilled && !wasKilled {
			msg := systemKillSwitchMessage{Type: "kill_switch", Reason: activeUnresolvedReason(events)}
			if err := writeJSON(ctx, conn, msg); err != nil {
				return err
			}
		}
		wasKilled = state == domain.SystemStateKilled
		return nil
	})
}

// activeUnresolvedReason returns the first still-unresolved event's
// reason, or defaultManualKillReason if events has none (a manual Kill
// Switch).
func activeUnresolvedReason(events []domain.KillSwitchEvent) string {
	for _, ev := range events {
		if ev.ResolvedAt == nil {
			return ev.Reason
		}
	}
	return defaultManualKillReason
}

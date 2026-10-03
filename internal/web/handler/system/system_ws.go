package system

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// systemKillSwitchMessage mirrors docs/api/endpoints.md §6's
// `{"type":"kill_switch","reason":"daily_loss_limit"}` message.
type systemKillSwitchMessage struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// systemStateChangedMessage is `{"type":"state_changed","state":"running"}`
// (docs/api/endpoints.md §6): pushed for every transition that does not
// end in Killed - Killed→Running by AutoResume or another window's Resume,
// and Running↔Paused from another window - so an open screen can resync.
type systemStateChangedMessage struct {
	Type  string             `json:"type"`
	State domain.SystemState `json:"state"`
}

// defaultManualKillReason is the fallback pushed when the system is Killed
// but has no unresolved kill_switch_events row. A manual `POST /system/kill`
// (FR-RISK-4) normally records an operator_manual row (risk.Engine.Kill), so
// this is only reached when the system.killed runtime_settings flag was set
// but recording the kill_switch_events row failed.
const defaultManualKillReason = "manual"

// WebSocket implements `/ws/system` (docs/api/endpoints.md §6,
// components/overview.md §5.4): pushes a `kill_switch` message the
// moment SystemEngine.State transitions into domain.SystemStateKilled -
// whether triggered by `POST /system/kill` or directly by Risk Engine
// (architecture/overview.md §10.3) - and a `state_changed` message on every
// other transition (notably Killed→Running by AutoResume), so
// `pitha-kill-switch-panel` and Header can follow state changes that did not
// originate from the panel's own POST call.
func (h *SystemHandler) WebSocket(c *gin.Context) {
	// previous is "" until the first successful read: a connection that
	// opens while already Killed is told so (kill_switch), but one that
	// opens while Running/Paused has nothing to report.
	var previous domain.SystemState
	shared.PollWebSocket(c, func() time.Duration { return h.pollInterval }, func(ctx context.Context, conn *websocket.Conn) error {
		state, events, err := h.engine.State(ctx)
		if err != nil {
			return shared.Transient(err)
		}

		switch {
		case state == previous:
		case state == domain.SystemStateKilled:
			msg := systemKillSwitchMessage{Type: "kill_switch", Reason: activeUnresolvedReason(events)}
			if err := shared.WriteJSON(ctx, conn, msg); err != nil {
				return err
			}
		case previous != "":
			msg := systemStateChangedMessage{Type: "state_changed", State: state}
			if err := shared.WriteJSON(ctx, conn, msg); err != nil {
				return err
			}
		}
		previous = state
		return nil
	})
}

// activeUnresolvedReason returns the first still-unresolved event's
// reason, or defaultManualKillReason if events has none unresolved.
func activeUnresolvedReason(events []domain.KillSwitchEvent) string {
	for _, ev := range events {
		if ev.ResolvedAt == nil {
			return ev.Reason
		}
	}
	return defaultManualKillReason
}

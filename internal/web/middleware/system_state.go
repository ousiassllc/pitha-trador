package middleware

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// SystemStateReader is the internal/service/risk.Engine method
// SystemState calls to learn the current Running/Paused/Killed state.
type SystemStateReader interface {
	State(ctx context.Context) (domain.SystemState, []domain.KillSwitchEvent, error)
}

type systemStateContextKey struct{}

// SystemStateFrom returns the current system state of the request ctx
// went through SystemState for, or "" when there is none (no middleware
// installed, or the state could not be read). Only layout.Shell's
// Header renders it, so the state is read lazily - at most once per
// full-page render, never for fragments/API/WebSocket requests.
func SystemStateFrom(ctx context.Context) domain.SystemState {
	read, _ := ctx.Value(systemStateContextKey{}).(func(context.Context) domain.SystemState)
	if read == nil {
		return ""
	}
	return read(ctx)
}

// SystemState returns Gin middleware exposing reader's current state to
// templates via SystemStateFrom, so organisms.Header can server-render
// the Kill Switch panel's status and which actions are currently allowed
// (HATEOAS: the server, not the client, decides). A read error yields ""
// (unknown): Header then renders no action buttons and lets the panel's
// own status fetch fill them in.
func SystemState(reader SystemStateReader) gin.HandlerFunc {
	read := func(ctx context.Context) domain.SystemState {
		state, _, err := reader.State(ctx)
		if err != nil {
			return ""
		}
		return state
	}
	return func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), systemStateContextKey{}, read)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

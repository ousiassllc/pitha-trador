package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// HeartbeatRecorder is the internal/service/risk.Engine method this
// middleware calls (FR-RISK-6). An interface here keeps
// internal/web/middleware from depending on internal/repository directly
// (docs/architecture/overview.md §3 layer rule: web depends on service,
// not repository).
type HeartbeatRecorder interface {
	RecordHeartbeat(ctx context.Context, at time.Time) error
}

// Heartbeat returns Gin middleware that records "now" as the operator's
// latest UI heartbeat on every request it wraps (functional.md FR-RISK-6:
// "認証済みUIリクエストのたびに last_ui_heartbeat_at を更新する"). The
// caller attaches it only to authenticated route groups once
// authentication middleware exists (docs/api/endpoints.md §1); it does
// not itself check authentication.
//
// A recording failure is logged and otherwise ignored: a heartbeat write
// error must not block the request it accompanies (Risk Engine's own
// dead-man's switch read side, Engine.CheckHeartbeatTimeout, degrades
// safely toward a Kill Switch if heartbeats stop landing, rather than
// this middleware failing the request itself).
func Heartbeat(recorder HeartbeatRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := recorder.RecordHeartbeat(c.Request.Context(), time.Now()); err != nil {
			slog.Error("middleware: record operator heartbeat", "error", err)
		}
		c.Next()
	}
}

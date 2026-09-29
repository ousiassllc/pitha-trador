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

// backgroundPollPaths are routes the page fetches by itself on a timer
// (`hx-trigger="every 60s"`, organisms/header.templ). They run whether or
// not anyone is at the screen, so they must not count as operator activity.
var backgroundPollPaths = map[string]bool{
	"/system/update-status": true,
}

// Heartbeat returns Gin middleware that records "now" as the operator's
// latest UI heartbeat for every authenticated UI request it wraps
// (functional.md FR-RISK-6: "認証済みUIリクエストのたびに
// last_ui_heartbeat_at を更新する"). It must be installed after
// Session.Handler, whose Authenticated flag it relies on: only requests
// carrying the valid session cookie count, so an unauthenticated probe
// cannot keep the dead-man's switch alive. Requests that are not operator
// activity are skipped: `/static/...`, WebSocket upgrades (opened and
// re-opened automatically by the page), and background timer polls
// (backgroundPollPaths).
//
// A recording failure is logged and otherwise ignored: a heartbeat write
// error must not block the request it accompanies (Risk Engine's own
// dead-man's switch read side, Engine.CheckHeartbeatTimeout, degrades
// safely toward a Kill Switch if heartbeats stop landing, rather than
// this middleware failing the request itself).
func Heartbeat(recorder HeartbeatRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if countsAsOperatorActivity(c) {
			if err := recorder.RecordHeartbeat(c.Request.Context(), time.Now()); err != nil {
				slog.Error("middleware: record operator heartbeat", "error", err)
			}
		}
		c.Next()
	}
}

func countsAsOperatorActivity(c *gin.Context) bool {
	path := c.Request.URL.Path
	return Authenticated(c.Request.Context()) &&
		!isStaticPath(path) &&
		!isWebSocketUpgrade(c.Request) &&
		!backgroundPollPaths[path]
}

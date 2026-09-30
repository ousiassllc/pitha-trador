package middleware

import (
	"context"
	"log/slog"
	"sync/atomic"
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

// BackgroundHeader (value "1") marks a request the page fires by itself, not as a
// consequence of an operator action: a Kill Switch push or WebSocket
// reconnect making pitha-kill-switch-panel resync `GET /api/v1/system/status`,
// and the `systemStateChanged`-triggered `GET /system/status` refresh of
// Header. Such requests run whether or not anyone is at the screen, so
// they must not extend the dead-man's switch (FR-RISK-6): otherwise the
// push that follows an operator_heartbeat_timeout Kill Switch would
// itself refresh the heartbeat and let AutoResume lift it unattended.
const (
	BackgroundHeader = "X-Pitha-Background"
	backgroundValue  = "1"
)

// heartbeatMinInterval bounds how often the heartbeat is persisted. The
// dead-man's switch threshold is measured in minutes, so refreshing at
// most every 10s loses nothing while sparing SQLite (single writer shared
// with the Scheduler and Risk Engine) a write per authenticated request.
const heartbeatMinInterval = 10 * time.Second

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
	return newHeartbeat(recorder, heartbeatMinInterval, time.Now)
}

func newHeartbeat(recorder HeartbeatRecorder, minInterval time.Duration, now func() time.Time) gin.HandlerFunc {
	var lastRecorded atomic.Int64 // UnixNano of the last persisted heartbeat; 0 = none yet
	return func(c *gin.Context) {
		if countsAsOperatorActivity(c) {
			at := now()
			prev := lastRecorded.Load()
			due := prev == 0 || at.Sub(time.Unix(0, prev)) >= minInterval
			// CAS so that concurrent requests in one interval write once.
			if due && lastRecorded.CompareAndSwap(prev, at.UnixNano()) {
				if err := recorder.RecordHeartbeat(c.Request.Context(), at); err != nil {
					slog.Error("middleware: record operator heartbeat", "error", err)
					// Not persisted: let the next request retry immediately.
					lastRecorded.CompareAndSwap(at.UnixNano(), prev)
				}
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
		!backgroundPollPaths[path] &&
		c.GetHeader(BackgroundHeader) != backgroundValue
}

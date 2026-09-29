// Package middleware contains Gin middleware (CSRF protection, structured
// request logging, panic recovery, operator heartbeat recording, ...) shared
// across the routes registered by internal/router.
//
// heartbeat.go implements operator heartbeat recording (FR-RISK-6): it
// records each authenticated UI request (session.go's Authenticated flag),
// skipping /static, WebSocket upgrades and background timer polls, and is
// installed by internal/router.WithHeartbeatRecorder right after the
// session middleware.
// session.go implements the local session token cookie and CSRF protection
// (docs/api/endpoints.md §1). setup_guard.go implements the first-run Setup
// Guard. request_log.go writes one structured slog access-log record per
// request and recovery.go converts handler panics into a logged 500; both
// are installed first by internal/router.New (RequestLog before Recovery).
package middleware

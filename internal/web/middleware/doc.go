// Package middleware contains Gin middleware (CSRF protection, structured
// request logging, panic recovery, operator heartbeat recording, ...) shared
// across the routes registered by internal/router.
//
// heartbeat.go implements operator heartbeat recording (FR-RISK-6).
// session.go implements the local session token cookie and CSRF protection
// (docs/api/endpoints.md §1). setup_guard.go implements the first-run Setup
// Guard. The remaining concrete middleware (structured request logging,
// panic recovery) is introduced by later sub-scopes.
package middleware

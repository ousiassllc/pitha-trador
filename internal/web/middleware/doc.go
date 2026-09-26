// Package middleware contains Gin middleware (CSRF protection, structured
// request logging, panic recovery, operator heartbeat recording, ...) shared
// across the routes registered by internal/router.
//
// heartbeat.go implements operator heartbeat recording (FR-RISK-6). The
// remaining concrete middleware (CSRF protection, structured request
// logging, panic recovery) is introduced by later sub-scopes.
package middleware

// Package middleware contains Gin middleware (CSRF protection, structured
// request logging, panic recovery, operator heartbeat recording, ...) shared
// across the routes registered by internal/router.
//
// Concrete middleware is introduced by later sub-scopes; this file only
// establishes the package skeleton.
package middleware

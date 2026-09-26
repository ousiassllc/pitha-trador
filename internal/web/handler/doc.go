// Package handler contains the Gin HTTP handlers (scanner.go, symbol.go,
// performance.go, calibration.go, system.go, ...) that back the routes
// registered by internal/router.
//
// This package MUST depend only on internal/service and internal/domain. It
// MUST NOT depend on internal/repository directly. See
// docs/architecture/overview.md §3 for the layer dependency rules
// (handler → service → repository → domain).
//
// scanner.go is implemented; the remaining handlers are introduced by
// later sub-scopes.
package handler

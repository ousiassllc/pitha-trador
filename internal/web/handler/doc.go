// Package handler contains the small list/analysis Gin handlers
// (scanner.go, performance.go, calibration.go, policy_proposals.go,
// swagger.go) that back routes registered by internal/router.
//
// Everything else lives in responsibility-scoped subpackages:
//   - shared:   Toast/ErrorPage responders, PollWebSocket and WriteJSON
//     (a leaf every other handler package may import)
//   - symbol:   Symbol List/Detail/Page/Close and /ws/symbols/:symbol
//   - system:   System state, Kill Switch controls, /ws/system, update UI
//   - settings: /settings, /setup and credential save/delete
//   - activity: System Activity Log (GET /api/v1/activity, /ws/activity)
//
// Sibling subpackages MUST NOT import each other and subpackages MUST NOT
// import this package.
//
// This package tree MUST depend only on internal/service and internal/domain
// (plus Templ under internal/web). It MUST NOT depend on internal/repository
// directly. See docs/architecture/overview.md §3 for the layer dependency
// rules (handler → service → repository → domain).
package handler

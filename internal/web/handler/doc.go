// Package handler is the root of the Gin handler tree that backs routes
// registered by internal/router. It holds no code of its own: every handler
// lives in a responsibility-scoped subpackage:
//   - shared:   Toast/ErrorPage responders, PollWebSocket and WriteJSON
//     (a leaf every other handler package may import)
//   - symbol:   Symbol List/Detail/Page/Close and /ws/symbols/:symbol
//   - system:   System state, Kill Switch controls, /ws/system, update UI
//   - settings: /settings, /setup and credential save/delete
//   - activity: System Activity Log (GET /api/v1/activity, /ws/activity)
//   - scanner:  Scanner page, candidates API, scan funnel and /ws/scanner
//   - performance: Performance page (walk-forward backtest and actuals)
//   - calibration: Calibration page and GET /api/v1/calibration
//   - proposals:   GET /api/v1/policy-proposals
//   - swagger:  Swagger UI page
//
// Sibling subpackages MUST NOT import each other and subpackages MUST NOT
// import this package.
//
// This package tree MUST depend only on internal/service and internal/domain
// (plus Templ under internal/web). It MUST NOT depend on internal/repository
// directly. See docs/architecture/overview.md §3 for the layer dependency
// rules (handler → service → repository → domain).
package handler

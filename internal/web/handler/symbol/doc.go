// Package symbol serves the Symbol pages and APIs: GET /symbols/:symbol,
// POST /positions/:id/close, /ws/symbols/:symbol and GET /api/v1/symbols/*,
// /positions and /orders.
// It reads market and position data through the SymbolProvider port and
// depends on internal/service, internal/domain, internal/config (risk limits)
// and handler/shared (plus Templ under internal/web); it MUST NOT import
// sibling handler subpackages.
package symbol

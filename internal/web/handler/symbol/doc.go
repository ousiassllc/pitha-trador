// Package symbol serves the Symbol pages and APIs: GET /symbols/:symbol,
// POST /positions/:id/close, /ws/symbols/:symbol and GET /api/v1/symbols/*,
// /positions and /orders.
// It reads market and position data through the SymbolProvider port and
// depends only on internal/service, internal/domain and handler/shared;
// it MUST NOT import sibling handler subpackages.
package symbol

// Package performance serves the Performance page (GET /performance):
// the walk-forward backtest (internal/service/backtest) and the actuals from
// internal/service/insight.
// It depends only on internal/service and handler/shared (plus Templ under
// internal/web); it MUST NOT import sibling handler subpackages.
package performance

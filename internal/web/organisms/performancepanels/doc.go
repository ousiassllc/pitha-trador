// Package performancepanels renders the Performance page's actuals panel,
// the Walk Forward backtest form, the backtest summary panel and the fold
// table. It takes plain, already formatted props (internal/web/handler/performance
// maps internal/service onto them), so it stays decoupled from
// internal/service. It is its own package because internal/web/organisms is
// at its line limit.
package performancepanels

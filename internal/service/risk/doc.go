// Package risk is the Risk Engine (docs/architecture/overview.md §4,
// functional.md §4.7): it enforces Paper/Live position-size and loss
// limits on every candidate trade (FR-RISK-1, engine.go's Check, the
// internal/service/policy.RiskChecker implementation), and owns the
// system-wide Running/Paused/Killed state machine driving Kill Switch
// activation, resolution and the operator dead-man's switch
// (FR-RISK-2〜7).
//
// Position/order data (open position count, consecutive losses, last
// loss) comes through PortfolioProvider (portfolio.go), backed in
// production by RepositoryPortfolioProvider over the positions table
// Paper Trading Execution writes. Actually closing positions when Kill
// Switch fires is Execution's job (closer.go's PositionCloser,
// internal/service/execution.Engine in production). Detecting
// kabuステーションAPI/Jev API health is internal/service/marketdata's
// and internal/service/jev's job (health.go's HealthChecker); no real
// HealthChecker is wired yet, so AlwaysHealthy keeps FR-RISK-7's
// auto-resume health gate permissive until one is.
package risk

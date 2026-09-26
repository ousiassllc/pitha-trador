// Package risk is the Risk Engine (docs/architecture/overview.md §4,
// functional.md §4.7): it enforces Paper/Live position-size and loss
// limits on every candidate trade (FR-RISK-1, engine.go's Check, the
// internal/service/policy.RiskChecker implementation), and owns the
// system-wide Running/Paused/Killed state machine driving Kill Switch
// activation, resolution and the operator dead-man's switch
// (FR-RISK-2〜7).
//
// Position/order data (open position count, exposure, daily P&L,
// consecutive losses) does not exist yet - Paper Trading Execution and
// the Symbol/Position API are later sub-scopes this package's own scope
// note defers them to. PortfolioProvider (portfolio.go) is the extension
// point a later sub-scope substitutes a real implementation for, mirroring
// how internal/service/policy.RiskChecker itself was this package's own
// extension point before this scope existed. Likewise, actually closing
// positions when Kill Switch fires is Execution's job (closer.go's
// PositionCloser), and detecting kabuステーションAPI/Jev API health is
// internal/service/marketdata's and internal/service/jev's job
// (health.go's HealthChecker) - none of those are wired to real
// implementations yet, so Engine's placeholders keep the corresponding
// FR-RISK-1/2/7 checks correct-but-inert (never trigger) until they are.
package risk

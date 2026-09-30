// Package execution implements Paper Trading Execution (functional.md
// §4.8 Entry/Exit, §4.9 状態管理): Paper Entry (market/limit,
// FR-ENTRY-1〜2), FR-EXIT-1's eight exit conditions evaluated together
// (fixed Stop Loss, fixed Take Profit, Trailing Stop, Jev方向反転,
// continuation_probability低下, VWAP逆クロス, max holding time, 引け前強
// 制決済), and the per-symbol state read model (§4.9) Symbol Detail's API
// (internal/web/handler/symbol.SymbolHandler) serves.
//
// Engine persists every Entry/Exit as a paper_orders row
// (trading.OrderRepository) and the resulting held/closed position as
// a positions row (trading.PositionRepository) - the same
// positions internal/service/risk/repoportfolio.Provider reads.
// Engine's CloseAll (close.go) implements that package's PositionCloser
// (FR-RISK-3); internal/bootstrap passes the same *Engine to
// risk.NewEngine as its Closer. OnSnapshot (manage.go) is the per-bar
// step internal/bootstrap's market-data job runs after persisting each
// new snapshot: limit-entry fills, mark-to-market and FR-EXIT-1 exits.
//
// Engine only opens positions for signals Policy Engine already passed
// through Risk Engine (domain.TradeSignal.RiskPassed == true,
// internal/service/policy.Engine.Decide) - Execution does not
// re-implement FR-RISK-1's limit checks itself, matching issue #37's
// "前提: #36（Risk Engine: 新規エントリー拒否判定...を参照するため）".
//
// Trailing Stop has no dedicated positions column for a running peak/
// trough price; Engine derives it on demand from
// market.SnapshotRepository.ListByInstrumentRange over
// [position.OpenedAt, now) instead of requiring a schema change outside
// this sub-scope's target files.
//
// jev_decisions has no queryable columns for Regime/EntryQuality/
// ToxicFlow/LiquidityStressed/ContinuationProbability (only
// response_json holds them - see internal/service/backtest's
// DecisionSource doc comment for the same limitation), so EvaluateExit
// takes a caller-supplied *domain.JevDecision with those fields already
// populated (parsed from response_json via EnrichDecision, decision.go)
// rather than reading judgement.DecisionRepository itself.
package execution

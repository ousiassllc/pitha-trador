// Package backtest is the Backtest Engine (docs/requirements/
// functional.md §4.11, docs/architecture/overview.md §4 "Backtest Engine
// （§4.11再利用）"): it replays Feature Engine (internal/service/
// featureengine) and Policy Engine (internal/service/policy) logic over
// historical market_snapshots data (internal/repository/market.
// SnapshotRepository.ListByInstrumentRange), computes trade-level
// outcomes, and aggregates them into the metrics FR-BT-1 requires
// (trade count, win rate, average profit/loss, Profit Factor,
// Expectancy, Max Drawdown, slippage-inclusive/fee-inclusive PnL).
//
// Two entry points cover this package's two callers:
//
//   - Run executes a full Training/Calibration → Validation → Forward
//     Walk Forward evaluation (FR-BT-2): the individual-trader UI's
//     "バックテスト実行" (UC-12), verifying a strategy end-to-end before
//     Paper Trading begins.
//   - ShadowBacktest runs a single out-of-sample period with a caller-
//     supplied policy.Thresholds override and no calibration split -
//     the "直近N営業日相当・提案後しきい値でのシャドーバックテスト" a
//     Self-Improvement Governor (internal/service/selfimprove) calls
//     before deciding whether to apply a Sol-proposed
//     threshold change (overview.md §8's `GOV->>BT` / `BT-->>GOV`).
//
// FR-BT-3 (look-ahead防止) is enforced twice: featureengine.Compute
// itself never uses a History bar timestamped after its Timestamp
// (FR-FE-1), and this package's own VerifyNoLookahead independently
// recomputes every bar's history-dependent Feature values from the same
// bounded featureengine.HistoryLookbackBars history before it and rejects a Run/ShadowBacktest call whose input data
// disagrees - so a look-ahead leak is caught here even if it originated
// outside the live Feature Engine pipeline (e.g. an externally-imported
// historical dataset).
//
// The replay follows the live decision flow (issue #775,
// docs/requirements/functional/components-platform.md FR-BT-4). Recorded
// (replayed): Jev Trader decisions, bar features, stored meta-labeling
// models. Recomputed: the hard guards and the meta-labeling take/skip
// (policy.Engine.DecideReplay, with the model trained before each decision -
// metahistory; none in force means no entry), the Risk Engine (simrisk, over
// a simulated portfolio; state a replay cannot know is treated as inactive),
// fills (fillmodel) and every FR-EXIT-1 exit condition in the live priority
// order (execution/exitcond, the function Paper Trading itself uses).
// RunConfig.Entry's legacy confidence rule is an explicitly named comparison
// mode (CompareEntryRules), never the default. parity compares a replay with
// the actual Paper trades of a period.
package backtest

package backtest

import (
	"context"
	"fmt"
)

// ShadowBacktest runs a single out-of-sample-period backtest - the
// "直近N営業日相当・提案後しきい値でのシャドーバックテスト" a
// Self-Improvement Governor calls (overview.md §8 "GOV->>BT") - reusing
// Run's exact same look-ahead-safe replay but without a
// Training/Validation split: the Governor already fixes the proposed
// policy.Thresholds itself (cfg.Thresholds), so there is nothing left
// for this call to calibrate. Every bar in period is evaluated with
// entries allowed. The caller compares the returned Metrics'
// Expectancy/MaxDrawdownPct against its pre-proposal baseline
// (overview.md §8 "BT-->>GOV: Expectancy / Max Drawdown比較結果").
func ShadowBacktest(ctx context.Context, cfg RunConfig, period Period) (Metrics, error) {
	trades, err := ShadowBacktestPortfolioTrades(ctx, []RunConfig{cfg}, period)
	if err != nil {
		return Metrics{}, err
	}
	return Aggregate(trades), nil
}

// ShadowBacktestTrades is ShadowBacktest without the final Aggregate
// step: it returns the raw []Trade replay produced over period for one
// instrument. With a Risk Engine (cfg.Risk) the portfolio is that one
// instrument alone, so portfolio-wide limits (max_open_positions, same
// direction) cannot bind: a multi-instrument replay uses
// ShadowBacktestPortfolioTrades.
func ShadowBacktestTrades(ctx context.Context, cfg RunConfig, period Period) ([]Trade, error) {
	return ShadowBacktestPortfolioTrades(ctx, []RunConfig{cfg}, period)
}

// ShadowBacktestPortfolioTrades replays every instrument of cfgs over
// period as one portfolio - the shadow backtest FR-SELFIMPROVE-4's
// Expectancy/MaxDrawdownPct figures need: trades of all instruments in
// chronological order under one Risk Engine (max_open_positions, same-
// direction cap, daily loss ... are portfolio-wide), not each
// instrument in isolation. Every cfg's Snapshots must pass
// VerifyNoLookahead (FR-BT-3).
func ShadowBacktestPortfolioTrades(ctx context.Context, cfgs []RunConfig, period Period) ([]Trade, error) {
	for _, cfg := range cfgs {
		if violations := VerifyNoLookahead(cfg.Snapshots, cfg.WarmupBars); len(violations) > 0 {
			return nil, fmt.Errorf("backtest: %s: %d bar(s) fail FR-BT-3 look-ahead check, e.g. %s", cfg.Symbol, len(violations), violations[0])
		}
	}
	return replay(ctx, cfgs, period, true)
}

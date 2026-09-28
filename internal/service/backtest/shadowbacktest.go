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
// Calibrated=true. The caller compares the returned Metrics'
// Expectancy/MaxDrawdownPct against its pre-proposal baseline
// (overview.md §8 "BT-->>GOV: Expectancy / Max Drawdown比較結果").
func ShadowBacktest(ctx context.Context, cfg RunConfig, period Period) (Metrics, error) {
	if violations := VerifyNoLookahead(cfg.Snapshots, cfg.WarmupBars); len(violations) > 0 {
		return Metrics{}, fmt.Errorf("backtest: %d bar(s) fail FR-BT-3 look-ahead check, e.g. %s", len(violations), violations[0])
	}
	trades, err := replay(ctx, cfg, period, true)
	if err != nil {
		return Metrics{}, err
	}
	return Aggregate(trades), nil
}

// ShadowBacktestTrades is ShadowBacktest without the final Aggregate
// step: it returns the raw []Trade replay produced over period, letting
// a caller combine trades from more than one instrument's RunConfig
// (each backtest.RunConfig covers a single instrument, DecisionSource's
// own doc comment) into one Aggregate call before comparing Metrics -
// exactly what a Self-Improvement Governor's multi-instrument shadow
// backtest needs (FR-SELFIMPROVE-4's "直近の...20営業日相当"
// Expectancy/MaxDrawdownPct figures must reflect every instrument's
// trades combined in chronological order, not each instrument averaged
// in isolation).
func ShadowBacktestTrades(ctx context.Context, cfg RunConfig, period Period) ([]Trade, error) {
	if violations := VerifyNoLookahead(cfg.Snapshots, cfg.WarmupBars); len(violations) > 0 {
		return nil, fmt.Errorf("backtest: %d bar(s) fail FR-BT-3 look-ahead check, e.g. %s", len(violations), violations[0])
	}
	return replay(ctx, cfg, period, true)
}

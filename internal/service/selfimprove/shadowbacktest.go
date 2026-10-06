package selfimprove

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// runShadowBacktests replays every instrument over period twice - once
// exactly as g.source supplied it (the baseline pass, already carrying
// the current policy.* thresholds) and once with its Thresholds.Policy
// replaced by candidate (FR-SELFIMPROVE-4) - and aggregates each pass's
// trades into one backtest.Metrics. Both passes share one
// ForEachRunConfig iteration, so the history is read from the DB once
// per proposal and only one instrument's bars are held at a time.
func (g *Governor) runShadowBacktests(ctx context.Context, period backtest.Period, candidate config.PolicyConfig) (baseline, candidateMetrics backtest.Metrics, err error) {
	var baselineTrades, candidateTrades []backtest.Trade
	err = g.source.ForEachRunConfig(ctx, period, func(cfg backtest.RunConfig) error {
		trades, err := backtest.ShadowBacktestTrades(ctx, cfg, period)
		if err != nil {
			return fmt.Errorf("selfimprove: baseline shadow backtest replay for %s: %w", cfg.Symbol, err)
		}
		baselineTrades = append(baselineTrades, trades...)

		cfg.Thresholds.Policy = candidate
		trades, err = backtest.ShadowBacktestTrades(ctx, cfg, period)
		if err != nil {
			return fmt.Errorf("selfimprove: candidate shadow backtest replay for %s: %w", cfg.Symbol, err)
		}
		candidateTrades = append(candidateTrades, trades...)
		return nil
	})
	if err != nil {
		return backtest.Metrics{}, backtest.Metrics{}, fmt.Errorf("selfimprove: shadow backtest: %w", err)
	}
	return backtest.Aggregate(baselineTrades), backtest.Aggregate(candidateTrades), nil
}

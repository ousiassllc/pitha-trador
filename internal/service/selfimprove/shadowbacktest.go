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
// trades into one backtest.Metrics. Each pass replays all instruments as
// one portfolio, in chronological order under one simulated Risk Engine
// (max_open_positions and the same-direction cap are portfolio-wide), so
// every instrument's bars are held in memory at once, like a Walk Forward
// run; the history is still read from the DB once per proposal.
func (g *Governor) runShadowBacktests(ctx context.Context, period backtest.Period, candidate config.PolicyConfig) (baseline, candidateMetrics backtest.Metrics, err error) {
	var baselineConfigs []backtest.RunConfig
	err = g.source.ForEachRunConfig(ctx, period, func(cfg backtest.RunConfig) error {
		baselineConfigs = append(baselineConfigs, cfg)
		return nil
	})
	if err != nil {
		return backtest.Metrics{}, backtest.Metrics{}, fmt.Errorf("selfimprove: shadow backtest: %w", err)
	}
	baselineTrades, err := backtest.ShadowBacktestPortfolioTrades(ctx, baselineConfigs, period)
	if err != nil {
		return backtest.Metrics{}, backtest.Metrics{}, fmt.Errorf("selfimprove: baseline shadow backtest replay: %w", err)
	}

	candidateConfigs := make([]backtest.RunConfig, len(baselineConfigs))
	for i, cfg := range baselineConfigs {
		cfg.Thresholds.Policy = candidate
		candidateConfigs[i] = cfg
	}
	candidateTrades, err := backtest.ShadowBacktestPortfolioTrades(ctx, candidateConfigs, period)
	if err != nil {
		return backtest.Metrics{}, backtest.Metrics{}, fmt.Errorf("selfimprove: candidate shadow backtest replay: %w", err)
	}
	return backtest.Aggregate(baselineTrades), backtest.Aggregate(candidateTrades), nil
}

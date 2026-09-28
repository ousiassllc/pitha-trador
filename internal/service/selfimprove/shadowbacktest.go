package selfimprove

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// runShadowBacktest aggregates every instrument's g.source.RunConfigs
// trades over period into one backtest.Metrics (FR-SELFIMPROVE-4). When
// override is non-nil, every RunConfig's Thresholds.Policy is replaced
// with *override before replay (the candidate pass); a nil override
// replays each RunConfig exactly as g.source supplied it (the baseline
// pass, already carrying the current policy.* thresholds).
func (g *Governor) runShadowBacktest(ctx context.Context, period backtest.Period, override *config.PolicyConfig) (backtest.Metrics, error) {
	configs, err := g.source.RunConfigs(ctx, period)
	if err != nil {
		return backtest.Metrics{}, fmt.Errorf("selfimprove: shadow backtest run configs: %w", err)
	}

	var allTrades []backtest.Trade
	for _, cfg := range configs {
		if override != nil {
			cfg.Thresholds.Policy = *override
		}
		trades, err := backtest.ShadowBacktestTrades(ctx, cfg, period)
		if err != nil {
			return backtest.Metrics{}, fmt.Errorf("selfimprove: shadow backtest replay for %s: %w", cfg.Symbol, err)
		}
		allTrades = append(allTrades, trades...)
	}
	return backtest.Aggregate(allTrades), nil
}

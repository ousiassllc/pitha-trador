package backtestlab

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// Run replays period once per variant of req over one load of the source's
// RunConfigs and returns the result of every variant and the coverage of the
// data. The variants run one after the other. The result is a function of the
// stored data, the period and req alone: the same input gives the same bytes
// (no clock, no random numbers, no map order). ctx stops the replay: the
// engine asks the decision source at every bar, and the lab's wrapper of it
// returns ctx's error once ctx is done.
//
// The source's RunConfigs are not modified; every variant is replayed on a copy.
func Run(ctx context.Context, src ConfigSource, period backtest.Period, req Request) (Result, Coverage, error) {
	cfgs, err := src.RunConfigs(ctx, period)
	if err != nil {
		return Result{}, Coverage{}, fmt.Errorf("backtestlab: load run configs: %w", err)
	}
	for i := range cfgs {
		if cfgs[i].Decisions != nil {
			cfgs[i].Decisions = cancellable{cfgs[i].Decisions}
		}
	}
	coverage := coverageOf(cfgs)

	out := Result{Variants: make([]VariantResult, 0, len(req.Variants))}
	for i, v := range req.Variants {
		if err := ctx.Err(); err != nil {
			return Result{}, Coverage{}, err
		}
		trades, err := backtest.ShadowBacktestPortfolioTrades(ctx, v.Params.Apply(cfgs), period)
		if err != nil {
			return Result{}, Coverage{}, fmt.Errorf("backtestlab: replay variant %d: %w", i, err)
		}
		out.Variants = append(out.Variants, summarize(i, v, trades, req.ExtraCostBps))
	}
	markReferenceBest(out.Variants)
	return out, coverage, nil
}

// cancellable makes a decision source stop with ctx's error once ctx is done.
type cancellable struct{ backtest.DecisionSource }

func (c cancellable) Decision(ctx context.Context, instrumentID int64, ts time.Time) (domain.JevDecision, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.JevDecision{}, false, err
	}
	return c.DecisionSource.Decision(ctx, instrumentID, ts)
}

// coverageOf is the first and last evaluated snapshot over all instruments
// (the leading warm-up bars are history before the period, not coverage).
func coverageOf(cfgs []backtest.RunConfig) Coverage {
	var c Coverage
	for _, cfg := range cfgs {
		bars := cfg.Snapshots
		if cfg.WarmupBars >= len(bars) {
			continue
		}
		first, last := bars[cfg.WarmupBars].Timestamp, bars[len(bars)-1].Timestamp
		if c.FirstSnapshotAt == nil || first.Before(*c.FirstSnapshotAt) {
			c.FirstSnapshotAt = &first
		}
		if c.LastSnapshotAt == nil || last.After(*c.LastSnapshotAt) {
			c.LastSnapshotAt = &last
		}
	}
	return c
}

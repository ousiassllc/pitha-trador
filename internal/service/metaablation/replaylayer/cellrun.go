package replaylayer

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// cellRun is one cell replayed on one segment: the engine's trades and what the
// entry evaluation saw.
type cellRun struct {
	trades []backtest.Trade
	evals  []backtest.EntryEvaluation
}

// tradeKey identifies the trade a position opened by an evaluation closes into.
type tradeKey struct {
	instrument int64
	entry      time.Time
}

// replaySegment replays cfgs (one cell, one segment) as one portfolio from the
// segment's start: one continuous path.
func replaySegment(ctx context.Context, cfgs []backtest.RunConfig, seg SegmentSpec, run *cellRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	trades, err := backtest.ShadowBacktestPortfolioTrades(ctx, cfgs, backtest.Period{Start: seg.From, End: seg.To})
	if err != nil {
		return fmt.Errorf("replaylayer: segment %d: %w", seg.Index, err)
	}
	run.trades = trades
	return nil
}

// runCells builds the four cells' configs on seg, asserts they differ only in
// the axes and replays each. The results are in metaablation.Cells() order.
func (in Input) runCells(ctx context.Context, seg SegmentSpec, decisions backtest.DecisionSource) ([]cellRun, error) {
	cells := metaablation.Cells()
	runs := make([]cellRun, len(cells))
	cfgs := make([][]backtest.RunConfig, len(cells))
	for i, c := range cells {
		run := &runs[i]
		cfgs[i] = in.runConfigs(c, seg, decisions, func(ev backtest.EntryEvaluation) { run.evals = append(run.evals, ev) })
	}
	if err := assertCommon(cfgs); err != nil {
		return nil, err
	}
	for i := range cells {
		if err := replaySegment(ctx, cfgs[i], seg, &runs[i]); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

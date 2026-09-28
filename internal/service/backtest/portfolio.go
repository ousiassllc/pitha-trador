package backtest

import (
	"context"
	"fmt"
)

// RunPortfolio executes a full Walk Forward backtest (FR-BT-2) over every
// instrument in cfgs using wf's fold boundaries, combining all
// instruments' trades per period before aggregating - so each fold's (and
// Combined's) Expectancy/MaxDrawdownPct reflect the whole universe traded
// together in chronological order, not one instrument in isolation.
//
// During each fold's Training/Calibration period every bar is evaluated
// with policy.Input.Calibrated=false (this setup has not been calibrated
// yet, matching policy.ReasonNotCalibrated's live semantics) so it never
// produces a trade; Validation and Forward bars are evaluated with
// Calibrated=true. Every cfg's Snapshots must pass VerifyNoLookahead
// (FR-BT-3).
func RunPortfolio(ctx context.Context, cfgs []RunConfig, wf WalkForwardConfig) (Result, error) {
	for _, cfg := range cfgs {
		if violations := VerifyNoLookahead(cfg.Snapshots, cfg.WarmupBars); len(violations) > 0 {
			return Result{}, fmt.Errorf("backtest: %s: %d bar(s) fail FR-BT-3 look-ahead check, e.g. %s", cfg.Symbol, len(violations), violations[0])
		}
	}

	splits := wf.Splits()
	if len(splits) == 0 {
		return Result{}, fmt.Errorf("backtest: walk forward config produces no folds for [%s, %s)", wf.Start, wf.End)
	}

	var result Result
	var combinedTrades []Trade
	for _, sp := range splits {
		training, err := replayAll(ctx, cfgs, sp.Training, false)
		if err != nil {
			return Result{}, err
		}
		validation, err := replayAll(ctx, cfgs, sp.Validation, true)
		if err != nil {
			return Result{}, err
		}
		forward, err := replayAll(ctx, cfgs, sp.Forward, true)
		if err != nil {
			return Result{}, err
		}

		result.Splits = append(result.Splits, SplitResult{
			Split:      sp,
			Training:   Aggregate(training),
			Validation: Aggregate(validation),
			Forward:    Aggregate(forward),
		})
		combinedTrades = append(combinedTrades, forward...)
	}
	result.Combined = Aggregate(combinedTrades)
	return result, nil
}

// replayAll replays every cfg over window, returning all instruments'
// trades together.
func replayAll(ctx context.Context, cfgs []RunConfig, window Period, calibrated bool) ([]Trade, error) {
	var trades []Trade
	for _, cfg := range cfgs {
		t, err := replay(ctx, cfg, window, calibrated)
		if err != nil {
			return nil, err
		}
		trades = append(trades, t...)
	}
	return trades, nil
}

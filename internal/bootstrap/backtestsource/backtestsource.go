// Package backtestsource reads the persisted market_snapshots/jev_decisions
// history from the DB and assembles the backtest.RunConfig values the
// Backtest Engine replays (FR-BT-1/FR-BT-2). It lives under
// internal/bootstrap as composition-root glue between internal/repository
// and internal/service/backtest, selfimprove and the Performance page's
// backtest runner (docs/architecture/overview.md §3).
package backtestsource

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Source assembles backtest.RunConfig values from the persisted
// market_snapshots/jev_decisions history of every active instrument, so
// a Walk Forward backtest (the Performance page) or a shadow backtest
// (selfimprove.ShadowBacktestSource) replays exactly the data the live
// pipeline recorded, under the policy.* thresholds currently in effect.
type Source struct {
	instruments *market.InstrumentRepository
	snapshots   *market.SnapshotRepository
	decisions   *judgement.DecisionRepository
	thresholds  policy.Thresholds
	policy      policy.PolicySource
	exit        backtest.ExitRule
	// cost/sessions are Paper Trading's own fill assumption
	// (execution.Config.Fill/Calendar), so a backtest fills orders exactly
	// as Execution does (#509): spread, slippage, fees, tick grid, 昼休み,
	// 寄り/引け.
	cost     fillmodel.Model
	sessions backtest.Sessions
}

// New builds a Source replaying with thresholds' spread/turnover limits and
// current's live policy.* thresholds.
func New(instruments *market.InstrumentRepository, snapshots *market.SnapshotRepository, decisions *judgement.DecisionRepository, thresholds policy.Thresholds, current policy.PolicySource, exit execution.Config) *Source {
	return &Source{
		instruments: instruments,
		snapshots:   snapshots,
		decisions:   decisions,
		thresholds:  thresholds,
		policy:      current,
		exit: backtest.ExitRule{
			StopLossPct:   exit.StopLossPct,
			TakeProfitPct: exit.TakeProfitPct,
			MaxHolding:    time.Duration(exit.MaxHoldingMinutes) * time.Minute,
		},
		cost:     exit.Fill,
		sessions: exit.Calendar,
	}
}

// RunConfigs returns one RunConfig per active instrument covering period
// (see ForEachRunConfig). It holds every instrument's bars in memory at
// once, which Walk Forward's RunPortfolio needs (it replays every
// instrument per fold); callers that can process one instrument at a
// time (the shadow backtest) use ForEachRunConfig instead.
func (b *Source) RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	var configs []backtest.RunConfig
	err := b.ForEachRunConfig(ctx, period, func(cfg backtest.RunConfig) error {
		configs = append(configs, cfg)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return configs, nil
}

// ForEachRunConfig calls fn with one RunConfig per active instrument
// covering period, with the currently-active policy.* thresholds: its
// market_snapshots in [period.Start, period.End) preceded by the
// featureengine.HistoryLookbackBars bars before period.Start as
// look-ahead-check warmup, and its Jev Trader decisions in the same range
// (enrich.Decision-populated, since Policy Engine needs fields
// jev_decisions only stores inside response_json). Instruments with no
// bars in period are skipped. Each RunConfig is built only when fn is
// about to receive it, so a caller that does not retain it never holds
// more than one instrument's bars. Snapshots are read without
// raw_data_json (the replay never reads the board). An error from fn
// stops the iteration and is returned as is.
func (b *Source) ForEachRunConfig(ctx context.Context, period backtest.Period, fn func(backtest.RunConfig) error) error {
	instruments, err := b.instruments.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return fmt.Errorf("backtestsource: list active instruments for backtest: %w", err)
	}
	thresholds := b.thresholds
	if thresholds.Policy, err = b.policy.CurrentThresholds(ctx); err != nil {
		return fmt.Errorf("backtestsource: current policy thresholds for backtest: %w", err)
	}

	for _, inst := range instruments {
		bars, err := b.snapshots.ListHistoryByInstrumentRange(ctx, inst.ID, period.Start, period.End)
		if err != nil {
			return err
		}
		if len(bars) == 0 {
			continue
		}
		warmup, err := b.snapshots.ListHistoryByInstrumentBefore(ctx, inst.ID, period.Start, featureengine.HistoryLookbackBars)
		if err != nil {
			return err
		}
		slices.Reverse(warmup)

		decisions, err := b.decisions.ListByInstrumentRange(ctx, inst.ID, domain.JevDecisionTypeTrader, period.Start, period.End)
		if err != nil {
			return err
		}
		for i := range decisions {
			decisions[i] = enrich.Decision(decisions[i])
		}

		if err := fn(backtest.RunConfig{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Snapshots:    append(warmup, bars...),
			WarmupBars:   len(warmup),
			Decisions:    backtest.NewSliceDecisionSource(decisions),
			Thresholds:   thresholds,
			Exit:         b.exit,
			Cost:         b.cost,
			Sessions:     b.sessions,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RunWalkForward runs a Walk Forward backtest (FR-BT-2) over every active
// instrument's recorded history in [wf.Start, wf.End).
func (b *Source) RunWalkForward(ctx context.Context, wf backtest.WalkForwardConfig) (backtest.Result, error) {
	configs, err := b.RunConfigs(ctx, backtest.Period{Start: wf.Start, End: wf.End})
	if err != nil {
		return backtest.Result{}, err
	}
	return backtest.RunPortfolio(ctx, configs, wf)
}

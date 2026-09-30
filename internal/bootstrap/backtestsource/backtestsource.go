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
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// backtestCost is the slippage/fee assumption every backtest this build
// runs applies (FR-BT-1's スリッページ込み/手数料込みPnL). kabuステーションAPI
// を提供する三菱UFJ eスマート証券（旧auカブコム証券）の国内株式現物取引手数料は
// 2026-05-18の改定以降無料のため、FeeBps is 0; 5bps of slippage per side is a
// conservative allowance for crossing half of a typical liquid-TSE-stock
// spread with a market order.
var backtestCost = backtest.CostModel{SlippageBps: 5, FeeBps: 0}

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
	}
}

// RunConfigs returns one RunConfig per active instrument covering period,
// with the currently-active policy.* thresholds:
// its market_snapshots in [period.Start, period.End) preceded by the
// featureengine.HistoryLookbackBars bars before period.Start as
// look-ahead-check warmup, and its Jev Trader decisions in the same range
// (enrich.Decision-populated, since Policy Engine needs fields
// jev_decisions only stores inside response_json). Instruments with no
// bars in period are omitted.
func (b *Source) RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	instruments, err := b.instruments.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return nil, fmt.Errorf("backtestsource: list active instruments for backtest: %w", err)
	}
	thresholds := b.thresholds
	if thresholds.Policy, err = b.policy.CurrentThresholds(ctx); err != nil {
		return nil, fmt.Errorf("backtestsource: current policy thresholds for backtest: %w", err)
	}

	var configs []backtest.RunConfig
	for _, inst := range instruments {
		bars, err := b.snapshots.ListByInstrumentRange(ctx, inst.ID, period.Start, period.End)
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			continue
		}
		warmup, err := b.snapshots.ListByInstrumentBefore(ctx, inst.ID, period.Start, featureengine.HistoryLookbackBars)
		if err != nil {
			return nil, err
		}
		slices.Reverse(warmup)

		decisions, err := b.decisions.ListByInstrumentRange(ctx, inst.ID, domain.JevDecisionTypeTrader, period.Start, period.End)
		if err != nil {
			return nil, err
		}
		for i := range decisions {
			decisions[i] = enrich.Decision(decisions[i])
		}

		configs = append(configs, backtest.RunConfig{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Snapshots:    append(warmup, bars...),
			WarmupBars:   len(warmup),
			Decisions:    backtest.NewSliceDecisionSource(decisions),
			Thresholds:   thresholds,
			Exit:         b.exit,
			Cost:         backtestCost,
		})
	}
	return configs, nil
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

package bootstrap

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// backtestCost is the slippage/fee assumption every backtest this build
// runs applies (FR-BT-1's スリッページ込み/手数料込みPnL). SBI証券's
// online domestic-equity trades carry no commission (ゼロ革命), so FeeBps
// is 0; 5bps of slippage per side is a conservative allowance for
// crossing half of a typical liquid-TSE-stock spread with a market order.
var backtestCost = backtest.CostModel{SlippageBps: 5, FeeBps: 0}

// BacktestSource assembles backtest.RunConfig values from the persisted
// market_snapshots/jev_decisions history of every active instrument, so
// a Walk Forward backtest (the Performance page) or a shadow backtest
// replays exactly the data the live pipeline recorded.
type BacktestSource struct {
	instruments *repository.InstrumentRepository
	snapshots   *repository.SnapshotRepository
	decisions   *repository.DecisionRepository
	thresholds  policy.Thresholds
	exit        backtest.ExitRule
}

func newBacktestSource(instruments *repository.InstrumentRepository, snapshots *repository.SnapshotRepository, decisions *repository.DecisionRepository, thresholds policy.Thresholds, exit execution.Config) *BacktestSource {
	return &BacktestSource{
		instruments: instruments,
		snapshots:   snapshots,
		decisions:   decisions,
		thresholds:  thresholds,
		exit: backtest.ExitRule{
			StopLossPct:   exit.StopLossPct,
			TakeProfitPct: exit.TakeProfitPct,
			MaxHolding:    time.Duration(exit.MaxHoldingMinutes) * time.Minute,
		},
	}
}

// RunConfigs returns one RunConfig per active instrument covering period:
// its market_snapshots in [period.Start, period.End) preceded by the
// featureengine.HistoryLookbackBars bars before period.Start as
// look-ahead-check warmup, and its Jev Trader decisions in the same range
// (EnrichDecision-populated, since Policy Engine needs fields
// jev_decisions only stores inside response_json). Instruments with no
// bars in period are omitted.
func (b *BacktestSource) RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	instruments, err := b.instruments.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: list active instruments for backtest: %w", err)
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
			decisions[i] = execution.EnrichDecision(decisions[i])
		}

		configs = append(configs, backtest.RunConfig{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Snapshots:    append(warmup, bars...),
			WarmupBars:   len(warmup),
			Decisions:    backtest.NewSliceDecisionSource(decisions),
			Thresholds:   b.thresholds,
			Exit:         b.exit,
			Cost:         backtestCost,
		})
	}
	return configs, nil
}

// RunWalkForward runs a Walk Forward backtest (FR-BT-2) over every active
// instrument's recorded history in [wf.Start, wf.End).
func (b *BacktestSource) RunWalkForward(ctx context.Context, wf backtest.WalkForwardConfig) (backtest.Result, error) {
	configs, err := b.RunConfigs(ctx, backtest.Period{Start: wf.Start, End: wf.End})
	if err != nil {
		return backtest.Result{}, err
	}
	return backtest.RunPortfolio(ctx, configs, wf)
}

package backtestsource

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

type settingsStore map[string]string

func (s settingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

// TestSource_BacktestFollowsTheCostSettings is issue #779: the backtest prices
// its fills with the Settings cost settings Paper Trading reads, so raising the
// commission and the slippage changes the entry fill and lowers the NetReturn.
func TestSource_BacktestFollowsTheCostSettings(t *testing.T) {
	firstTrade := func(store settingsStore) backtest.Trade {
		t.Helper()
		env := newTestEnvWith(t, func(cfg *execution.Config) {
			cfg.Calendar = marketcalendar.TSE
			cfg.Costs = fillmodel.SettingsSource{Store: store}
		})
		inst := mustCreateInstrument(t, env, "7203")
		base := time.Date(2026, 9, 1, 9, 0, 0, 0, marketcalendar.JST)
		recordLiveBars(t, env, inst, base, 30)
		period := backtest.Period{Start: base, End: base.Add(30 * time.Minute)}
		configs, err := env.Source.RunConfigs(context.Background(), period)
		if err != nil || len(configs) != 1 {
			t.Fatalf("RunConfigs = %d configs, err %v; want 1", len(configs), err)
		}
		trades, err := backtest.ShadowBacktestTrades(context.Background(), configs[0], period)
		if err != nil || len(trades) == 0 {
			t.Fatalf("ShadowBacktestTrades = %d trades, err %v; want at least 1", len(trades), err)
		}
		return trades[0]
	}
	cheap := firstTrade(settingsStore{})
	costly := firstTrade(settingsStore{"cost.fee_bps": "5", "cost.slippage_long_continuous_bps": "40", "cost.slippage_short_continuous_bps": "40", "cost.slippage_long_opening_bps": "40", "cost.slippage_short_opening_bps": "40"})

	if !(costly.NetReturnPct < cheap.NetReturnPct) {
		t.Fatalf("NetReturnPct = %v with the costlier settings, want below %v", costly.NetReturnPct, cheap.NetReturnPct)
	}
	if got, want := (costly.GrossReturnPct-costly.FeeReturnPct)-(cheap.GrossReturnPct-cheap.FeeReturnPct), 0.10; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("fee deduction rose by %v%%, want 0.10 (5 bps on both fills)", got)
	}
	if costly.Direction == domain.JevDirectionLong && !(costly.EntryPrice > cheap.EntryPrice) {
		t.Errorf("LONG entry fill %v, want above the default-cost fill %v", costly.EntryPrice, cheap.EntryPrice)
	}
}

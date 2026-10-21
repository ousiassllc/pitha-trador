package backtestsource

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Issue #824: the backtest's simulated Risk Engine gets the stock budget in
// force - the stored row, else the whole Paper capital - like the live engine.
func TestSource_BacktestFollowsTheStockBucketBudget(t *testing.T) {
	stockBudget := func(row string) float64 {
		t.Helper()
		env := newTestEnv(t)
		if row != "" {
			if err := system.NewRuntimeSettingsRepository(env.Conn).Set(context.Background(), bucketsettings.KeyStockBudgetJPY, row, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		inst := mustCreateInstrument(t, env, "7203")
		base := time.Date(2026, 9, 1, 9, 0, 0, 0, marketcalendar.JST)
		storeTakingModel(t, env, base.Add(-time.Hour))
		recordLiveBars(t, env, inst, base, 30)
		configs, err := env.Source.RunConfigs(context.Background(), backtest.Period{Start: base, End: base.Add(30 * time.Minute)})
		if err != nil || len(configs) != 1 || configs[0].Risk == nil || configs[0].Risk.StockBudgetJPY == nil {
			t.Fatalf("RunConfigs = %+v, err %v; want one config carrying a stock budget", configs, err)
		}
		return *configs[0].Risk.StockBudgetJPY
	}
	if got := stockBudget(""); got != 30_000_000 {
		t.Errorf("stock budget with no row = %v, want the whole Paper capital 30,000,000", got)
	}
	if got := stockBudget("0"); got != 0 {
		t.Errorf("stock budget with a stored 0 = %v, want 0", got)
	}
}

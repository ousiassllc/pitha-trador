package exitflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// Issue #824: a stock budget of 0 only stops new entries. A position held
// before the budget was lowered is still evaluated and closed by the usual exit
// rules: Exit never reads the bucket budgets.
func TestEngine_StockBucketZeroDoesNotHoldBackExit(t *testing.T) {
	ctx := context.Background()
	te := newTestEngine(t, execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2})
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	position := openLongPosition(t, te, 2100.0, openedAt)

	settings := system.NewRuntimeSettingsRepository(te.db)
	if err := settings.Set(ctx, bucketsettings.KeyStockBudgetJPY, "0", openedAt); err != nil {
		t.Fatal(err)
	}

	stopPrice := 2100.0 * (1 - 0.007)
	reason, triggered, err := te.engine.EvaluateExit(ctx, position, execution.MarketContext{Price: stopPrice, Now: openedAt.Add(time.Minute)})
	if err != nil || !triggered || reason != domain.ExitReasonStopLoss {
		t.Fatalf("EvaluateExit with the stock budget at 0 = (%q, %v, %v), want the stop loss to trigger", reason, triggered, err)
	}
	closed, err := te.engine.CloseAtMarket(ctx, position.ID, reason, openedAt.Add(2*time.Minute))
	if err != nil || closed.IsOpen() {
		t.Fatalf("CloseAtMarket with the stock budget at 0 = (open=%v, %v), want the position closed", closed.IsOpen(), err)
	}
}

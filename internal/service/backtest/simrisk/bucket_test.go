package simrisk_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

func budget(v float64) *float64 { return &v }

// limits(): capital 10,000,000, 10% per symbol, so one entry at 2,000 yen is
// 500 shares = 1,000,000 yen. These cases mirror checkflow's live ones
// (stock budget 0, budget below holdings + entry, budget exactly reached): the
// backtest and live Risk reject with the same codes at the same boundary.
func TestPortfolio_StockBudgetZeroRejectsEntryWithBucketDisabled(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits(), StockBudgetJPY: budget(0)})
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionLong, risk.ReasonBucketDisabled)
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionShort, risk.ReasonBucketDisabled)
}

func TestPortfolio_StockBudgetBelowHoldingsPlusEntryRejectsWithBucketBudgetExceeded(t *testing.T) {
	// 1,000,000 held (500 x 2,000) + this entry's 1,000,000.
	p := simrisk.New(simrisk.Config{Limits: limits(), StockBudgetJPY: budget(1_999_999)})
	p.Open(1, domain.PositionSideLong, 500, 2000)
	wantRejected(t, p, at(9, 30), 2, domain.JevDirectionLong, risk.ReasonBucketBudgetExceeded)

	// Exactly the budget is allowed.
	p = simrisk.New(simrisk.Config{Limits: limits(), StockBudgetJPY: budget(2_000_000)})
	p.Open(1, domain.PositionSideLong, 500, 2000)
	if ok, reason := check(p, at(9, 30), 2, domain.JevDirectionLong); !ok {
		t.Fatalf("Check at exactly the budget = (false, %q), want passed", reason)
	}
}

// The default split - no budget (nil) or the whole capital - never rejects what
// the percentage limits let through, so existing backtests are unchanged.
func TestPortfolio_DefaultStockBudgetIsTheWholeCapitalAndChangesNothing(t *testing.T) {
	for name, cfg := range map[string]simrisk.Config{
		"nil budget":           {Limits: limits()},
		"whole capital budget": {Limits: limits(), StockBudgetJPY: budget(limits().InitialCapital)},
	} {
		t.Run(name, func(t *testing.T) {
			p := simrisk.New(cfg)
			p.Open(1, domain.PositionSideLong, 500, 2000)
			if ok, reason := check(p, at(9, 30), 2, domain.JevDirectionLong); !ok {
				t.Fatalf("Check = (false, %q), want passed", reason)
			}
		})
	}
}

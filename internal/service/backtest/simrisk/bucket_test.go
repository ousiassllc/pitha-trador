package simrisk_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
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

// bucketLimits lets a deep unrealized loss through the other limits, so only the
// stock bucket's budget decides: capital 10,000,000, an entry at 2,000 yen is
// 500 shares = 1,000,000 yen.
func bucketLimits() config.RiskLimits {
	l := limits()
	l.MaxTotalExposurePct, l.MaxDailyLossPct = 100, 100
	return l
}

// Holdings are valued at the market, like the exposure limits: 5,000 shares
// bought at 2,000 (10,000,000 yen, the whole default budget) are worth
// 5,000,000 at the mark of 1,000, which leaves room that the entry price
// (10,000,000 + 1,000,000) would not.
func TestPortfolio_DefaultBudgetCountsHoldingsAtMarketNotEntryPrice(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: bucketLimits()})
	p.Open(1, domain.PositionSideLong, 5000, 2000)
	p.Observe(1, 1000)
	if ok, reason := check(p, at(9, 30), 2, domain.JevDirectionLong); !ok {
		t.Fatalf("Check = (false, %q), want passed under the default budget", reason)
	}
}

// A budget below the capital rejects once the market value of the holdings plus
// the entry goes over it, and not before: 500 shares bought at 2,000, marked
// 1,900 = 950,000 held + this entry's 1,000,000.
func TestPortfolio_BudgetBelowCapitalRejectsOnMarketValueHoldings(t *testing.T) {
	open := func(budgetJPY float64) *simrisk.Portfolio {
		p := simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(budgetJPY)})
		p.Open(1, domain.PositionSideLong, 500, 2000)
		p.Observe(1, 1900)
		return p
	}
	wantRejected(t, open(1_949_999), at(9, 30), 2, domain.JevDirectionLong, risk.ReasonBucketBudgetExceeded)
	if ok, reason := check(open(1_950_000), at(9, 30), 2, domain.JevDirectionLong); !ok {
		t.Fatalf("Check at exactly the market-value total = (false, %q), want passed", reason)
	}
}

// setAssetClass puts instrumentID in its asset class's bucket.
func setAssetClass(t *testing.T, p *simrisk.Portfolio, instrumentID int64, assetClass string) {
	t.Helper()
	if err := p.SetAssetClass(instrumentID, assetClass); err != nil {
		t.Fatal(err)
	}
}

// A stock entry (issue #833) is judged on the stock bucket alone: the bond_etf
// bucket's budget and holdings neither admit nor hold it back. These cases mirror
// checkflow's live ones.
func TestPortfolio_StockEntryIsJudgedOnTheStockBucketOnly(t *testing.T) {
	// Stock budget 0 is not rescued by a big bond_etf budget.
	p := simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(0), BondETFBudgetJPY: budget(10_000_000)})
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionLong, risk.ReasonBucketDisabled)

	// 5,000,000 held in a bond ETF (instrument 3) does not count against the
	// stock budget of exactly this entry's 1,000,000.
	p = simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(1_000_000), BondETFBudgetJPY: budget(10_000_000)})
	setAssetClass(t, p, 3, domain.AssetClassBondETF)
	p.Open(3, domain.PositionSideLong, 2500, 2000)
	if ok, reason := check(p, at(9, 30), 1, domain.JevDirectionLong); !ok {
		t.Fatalf("stock Check with bond_etf holdings = (false, %q), want passed", reason)
	}

	// The stock bucket's own holdings count: 1,000,000 held + this entry's.
	p = simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(1_000_000), BondETFBudgetJPY: budget(10_000_000)})
	p.Open(2, domain.PositionSideLong, 500, 2000)
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionLong, risk.ReasonBucketBudgetExceeded)
}

// A bond ETF entry is judged on the bond_etf bucket alone: the stock budget and
// the stock holdings neither admit nor hold it back.
func TestPortfolio_BondETFEntryIsJudgedOnTheBondETFBucketOnly(t *testing.T) {
	// Default split: the bond_etf budget is 0, whatever the stock budget.
	p := simrisk.New(simrisk.Config{Limits: bucketLimits()})
	setAssetClass(t, p, 1, domain.AssetClassBondETF)
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionLong, risk.ReasonBucketDisabled)

	// Stock budget 0 and 5,000,000 held in stocks do not matter: a bond_etf
	// budget of exactly this entry's 1,000,000 passes.
	p = simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(0), BondETFBudgetJPY: budget(1_000_000)})
	setAssetClass(t, p, 1, domain.AssetClassBondETF)
	p.Open(2, domain.PositionSideLong, 2500, 2000)
	if ok, reason := check(p, at(9, 30), 1, domain.JevDirectionLong); !ok {
		t.Fatalf("bond_etf Check with a bond_etf budget = (false, %q), want passed", reason)
	}

	// The bond_etf bucket's own holdings count: 1,000,000 held + this entry's.
	p = simrisk.New(simrisk.Config{Limits: bucketLimits(), StockBudgetJPY: budget(0), BondETFBudgetJPY: budget(1_000_000)})
	setAssetClass(t, p, 1, domain.AssetClassBondETF)
	setAssetClass(t, p, 3, domain.AssetClassBondETF)
	p.Open(3, domain.PositionSideLong, 500, 2000)
	wantRejected(t, p, at(9, 30), 1, domain.JevDirectionLong, risk.ReasonBucketBudgetExceeded)
}

func TestPortfolio_SetAssetClassRejectsAnUnknownClass(t *testing.T) {
	p := simrisk.New(simrisk.Config{Limits: limits()})
	if err := p.SetAssetClass(1, "crypto"); err == nil {
		t.Fatal("SetAssetClass(crypto) = nil, want an error")
	}
	if err := p.SetAssetClass(1, ""); err != nil {
		t.Fatalf("SetAssetClass(empty) = %v, want a stock", err)
	}
}

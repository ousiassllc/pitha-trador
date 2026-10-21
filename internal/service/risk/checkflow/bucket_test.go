package checkflow_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// bucketEngine is an Engine over a fresh DB with one 2,000-yen snapshot
// (instrument 1) and the stock-bucket gates of a Paper Engine (withBucket) or
// none (a Live-style Engine). testLimits: capital 30,000,000, 2% per symbol, so
// an entry is 300 shares x 2,000 yen = 600,000 yen.
func bucketEngine(t *testing.T, withBucket bool) (*risk.Engine, *system.RuntimeSettingsRepository, *sql.DB) {
	t.Helper()
	return bucketEngineWith(t, withBucket, testLimits(), fakePortfolio{})
}

// bucketEngineWith is bucketEngine with the limits and the portfolio (the
// total exposure the percentage limits and sizing see) chosen by the test.
func bucketEngineWith(t *testing.T, withBucket bool, limits config.RiskLimits, portfolio fakePortfolio) (*risk.Engine, *system.RuntimeSettingsRepository, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, 5)
	settings := system.NewRuntimeSettingsRepository(db)
	positions := trading.NewPositionRepository(db)
	cfg := risk.Config{
		Limits:     limits,
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   settings,
		Snapshots:  snapshots,
		Portfolio:  portfolio,
	}
	if withBucket {
		cfg.Buckets = bucket.NewProvider(positions, settings, market.NewInstrumentRepository(db))
	}
	return risk.NewEngine(cfg), settings, db
}

func storeStockBudget(t *testing.T, s *system.RuntimeSettingsRepository, value string) {
	t.Helper()
	if err := s.Set(context.Background(), bucketsettings.KeyStockBudgetJPY, value, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func openStockPosition(t *testing.T, db *sql.DB, quantity int64, entryPrice float64) {
	t.Helper()
	openStockPositionMarked(t, db, quantity, entryPrice, entryPrice)
}

// openStockPositionMarked opens a stock position whose latest mark is currentPrice.
func openStockPositionMarked(t *testing.T, db *sql.DB, quantity int64, entryPrice, currentPrice float64) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	order, err := trading.NewOrderRepository(db).Insert(ctx, domain.PaperOrder{
		InstrumentID: 1, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: quantity, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	if _, err := trading.NewPositionRepository(db).Open(ctx, domain.Position{
		InstrumentID: 1, EntryOrderID: order.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: quantity, EntryPrice: entryPrice, CurrentPrice: currentPrice, OpenedAt: now,
	}); err != nil {
		t.Fatalf("open position: %v", err)
	}
}

func TestRisk_StockBucketZeroRejectsEntryWithBucketDisabled(t *testing.T) {
	ctx := context.Background()
	e, settings, _ := bucketEngine(t, true)
	storeStockBudget(t, settings, "0")

	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketDisabled) || risk.ReasonBucketDisabled != "bucket_disabled" {
		t.Fatalf("Check with the stock budget at 0 = (%v, %q), want (false, prefix bucket_disabled)", passed, reason)
	}
	// SHORT entries are stock entries too.
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionShort); passed || !strings.HasPrefix(reason, risk.ReasonBucketDisabled) {
		t.Fatalf("SHORT Check with the stock budget at 0 = (%v, %q), want bucket_disabled", passed, reason)
	}
}

func TestRisk_StockBucketBudgetLoweredRejectsExcessWithBucketBudgetExceeded(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngine(t, true)

	// 600,000 yen held (300 x 2,000) + this entry's 600,000 against a 1,000,000 budget.
	openStockPosition(t, db, 300, 2000)
	storeStockBudget(t, settings, "1000000")
	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketBudgetExceeded) || risk.ReasonBucketBudgetExceeded != "bucket_budget_exceeded" {
		t.Fatalf("Check over the budget = (%v, %q), want (false, prefix bucket_budget_exceeded)", passed, reason)
	}

	// Reaching the budget exactly is allowed: 600,000 + 600,000.
	storeStockBudget(t, settings, "1200000")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check at exactly the budget = (false, %q), want passed", reason)
	}
}

// The default split (stock = the whole capital, no stored row or the seeded
// row) and an Engine without a StockBucket (Live) decide exactly as before.
func TestRisk_DefaultBucketSplitAndLiveEngineAreUnchanged(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngine(t, true)
	openStockPosition(t, db, 300, 2000)
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check with no budget row = (false, %q), want passed (the budget is the whole capital)", reason)
	}
	storeStockBudget(t, settings, "30000000")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check with the seeded whole-capital budget = (false, %q), want passed", reason)
	}

	live, liveSettings, _ := bucketEngine(t, false)
	storeStockBudget(t, liveSettings, "0")
	if passed, reason := live.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Live-style Check (no StockBucket) = (false, %q), want no budget gate", reason)
	}
}

// The percentage limits keep measuring against initial_capital: a small stock
// budget does not change their denominator.
func TestRisk_StockBudgetDoesNotChangeThePercentLimitDenominator(t *testing.T) {
	ctx := context.Background()
	e, settings, _ := bucketEngine(t, true)
	storeStockBudget(t, settings, "600000")
	limits, err := e.EffectiveLimits(ctx)
	if err != nil || limits.InitialCapital != testLimits().InitialCapital {
		t.Fatalf("EffectiveLimits = %+v, %v; want the capital unchanged by the budget", limits, err)
	}
	if got := e.PositionSize(ctx, 2000); got != 300 {
		t.Fatalf("PositionSize with a 600,000 budget = %d, want 300 (sized against the capital)", got)
	}
}

// The stock bucket's holdings are valued at the market, like the exposure
// limits: an unrealized loss frees room that the entry price would still count.
// Capital 30,000,000 and the total exposure limit at 100%: 20,000 shares bought
// at 2,000 (40,000,000 yen at entry, over the default budget of the whole
// capital) marked 1,000 are worth 20,000,000 (66.67% of the capital, what the
// portfolio reports). The entry of 600,000 on top stays within the budget, so
// the default decides as before; at entry price (40,000,000 + 600,000) it
// would have been rejected.
func TestRisk_DefaultBudgetCountsHoldingsAtMarketNotEntryPrice(t *testing.T) {
	limits := testLimits()
	limits.MaxTotalExposurePct = 100
	e, _, db := bucketEngineWith(t, true, limits, fakePortfolio{totalExposurePct: 20_000_000.0 / 30_000_000 * 100})
	openStockPositionMarked(t, db, 20_000, 2000, 1000)

	if passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check = (false, %q), want passed under the default budget", reason)
	}
}

// A stock budget below the capital rejects once the market value of the
// holdings plus the entry goes over it, and not before.
func TestRisk_BudgetBelowCapitalRejectsOnMarketValueHoldings(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngineWith(t, true, testLimits(), fakePortfolio{totalExposurePct: 450_000.0 / 30_000_000 * 100})
	// 300 shares bought at 2,000, marked 1,500: 450,000 held + this entry's 600,000.
	openStockPositionMarked(t, db, 300, 2000, 1500)

	storeStockBudget(t, settings, "1049999")
	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketBudgetExceeded) || !strings.Contains(reason, "held_jpy=450000") {
		t.Fatalf("Check one yen under the market-value total = (%v, %q), want bucket_budget_exceeded with held_jpy=450000", passed, reason)
	}

	storeStockBudget(t, settings, "1050000")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check at exactly the market-value total = (false, %q), want passed", reason)
	}
}

// A position that has not been marked yet (current_price 0) is held at its
// entry price: 300 shares at 2,000 = 600,000 + this entry's 600,000.
func TestRisk_UnmarkedHoldingsCountAtEntryPrice(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngineWith(t, true, testLimits(), fakePortfolio{totalExposurePct: 600_000.0 / 30_000_000 * 100})
	openStockPositionMarked(t, db, 300, 2000, 0)

	storeStockBudget(t, settings, "1199999")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.Contains(reason, "held_jpy=600000") {
		t.Fatalf("Check one yen under = (%v, %q), want bucket_budget_exceeded with held_jpy=600000", passed, reason)
	}
	storeStockBudget(t, settings, "1200000")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check at exactly the total = (false, %q), want passed", reason)
	}
}

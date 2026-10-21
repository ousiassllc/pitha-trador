package checkflow_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// A stock is instrument 1 (bucketEngine); bondETFID is a bond ETF with the same
// 2,000-yen price, so either entry is 300 shares x 2,000 yen = 600,000 yen.
const bondETFID = 2

// addBondETF creates the bond_etf instrument (id bondETFID) with its snapshot.
func addBondETF(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{
		Symbol: "2511", Name: "Bond ETF", Market: "TSE", IsActive: true, AssetClass: domain.AssetClassBondETF,
	})
	if err != nil || inst.ID != bondETFID {
		t.Fatalf("create bond_etf instrument = (%+v, %v), want id %d", inst, err, bondETFID)
	}
	spread := 5.0
	if _, err := market.NewSnapshotRepository(db).Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID, Timestamp: time.Now(), Price: 2000, SpreadBps: &spread, RawDataJSON: "{}",
	}); err != nil {
		t.Fatalf("insert bond_etf snapshot: %v", err)
	}
}

func storeBudget(t *testing.T, s *system.RuntimeSettingsRepository, key, value string) {
	t.Helper()
	if err := s.Set(context.Background(), key, value, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// openBondETFPosition opens a bond_etf position (positions.bucket is taken from
// the instrument's asset_class) marked at its entry price.
func openBondETFPosition(t *testing.T, db *sql.DB, quantity int64, price float64) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	order, err := trading.NewOrderRepository(db).Insert(ctx, domain.PaperOrder{
		InstrumentID: bondETFID, Symbol: "2511", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: quantity, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert bond_etf entry order: %v", err)
	}
	if _, err := trading.NewPositionRepository(db).Open(ctx, domain.Position{
		InstrumentID: bondETFID, EntryOrderID: order.ID, Symbol: "2511", Side: domain.PositionSideLong,
		Quantity: quantity, EntryPrice: price, CurrentPrice: price, OpenedAt: now,
	}); err != nil {
		t.Fatalf("open bond_etf position: %v", err)
	}
}

// A stock entry is judged on the stock bucket alone: the bond_etf bucket's
// budget and holdings neither admit nor hold it back.
func TestRisk_StockEntryIsJudgedOnTheStockBucketOnly(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngine(t, true)
	addBondETF(t, db)

	// A big bond_etf budget does not rescue a stock bucket whose budget is 0 ...
	storeBudget(t, settings, bucketsettings.KeyBondETFBudgetJPY, "10000000")
	storeStockBudget(t, settings, "0")
	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketDisabled) || !strings.Contains(reason, "bucket=stock") {
		t.Fatalf("stock Check with stock budget 0 = (%v, %q), want bucket_disabled for bucket=stock", passed, reason)
	}

	// ... and bond_etf holdings do not count against the stock budget: 5,000,000
	// held there, a stock budget of exactly this entry's 600,000 still passes.
	openBondETFPosition(t, db, 2500, 2000)
	storeStockBudget(t, settings, "600000")
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("stock Check with 600000 stock budget and bond_etf holdings = (false, %q), want passed", reason)
	}

	// The stock bucket's own holdings still count: 600,000 held + 600,000.
	openStockPosition(t, db, 300, 2000)
	passed, reason = e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketBudgetExceeded) || !strings.Contains(reason, "bucket=stock held_jpy=600000") {
		t.Fatalf("stock Check over the stock budget = (%v, %q), want bucket_budget_exceeded for bucket=stock held_jpy=600000", passed, reason)
	}
}

// A bond ETF entry is judged on the bond_etf bucket alone: the stock bucket's
// budget and holdings neither admit nor hold it back.
func TestRisk_BondETFEntryIsJudgedOnTheBondETFBucketOnly(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngine(t, true)
	addBondETF(t, db)

	// Default split: the bond_etf budget is 0 (no stored row), so the entry is
	// disabled however large the stock budget is.
	storeStockBudget(t, settings, "30000000")
	passed, reason := e.Check(ctx, bondETFID, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketDisabled) || !strings.Contains(reason, "bucket=bond_etf budget_jpy=0") {
		t.Fatalf("bond_etf Check with the default bond_etf budget = (%v, %q), want bucket_disabled for bucket=bond_etf", passed, reason)
	}

	// Stock budget 0 and 5,000,000 held in stocks do not matter: the bond_etf
	// budget of exactly this entry's 600,000 passes.
	openStockPosition(t, db, 2500, 2000)
	storeStockBudget(t, settings, "0")
	storeBudget(t, settings, bucketsettings.KeyBondETFBudgetJPY, "600000")
	if passed, reason := e.Check(ctx, bondETFID, domain.JevDirectionLong); !passed {
		t.Fatalf("bond_etf Check with 600000 bond_etf budget = (false, %q), want passed", reason)
	}

	// The bond_etf bucket's own holdings count: 600,000 held + 600,000.
	openBondETFPosition(t, db, 300, 2000)
	passed, reason = e.Check(ctx, bondETFID, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonBucketBudgetExceeded) || !strings.Contains(reason, "bucket=bond_etf held_jpy=600000") {
		t.Fatalf("bond_etf Check over the bond_etf budget = (%v, %q), want bucket_budget_exceeded for bucket=bond_etf held_jpy=600000", passed, reason)
	}
	storeBudget(t, settings, bucketsettings.KeyBondETFBudgetJPY, "1200000")
	if passed, reason := e.Check(ctx, bondETFID, domain.JevDirectionLong); !passed {
		t.Fatalf("bond_etf Check at exactly the bond_etf budget = (false, %q), want passed", reason)
	}
}

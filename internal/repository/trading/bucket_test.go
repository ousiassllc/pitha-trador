package trading_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

func bucketOf(t *testing.T, db *sql.DB, table string, id int64) string {
	t.Helper()
	var bucket string
	if err := db.QueryRowContext(context.Background(), `SELECT bucket FROM `+table+` WHERE id = ?`, id).Scan(&bucket); err != nil {
		t.Fatalf("read %s.bucket: %v", table, err)
	}
	return bucket
}

// Issue #824: the bucket of a signal, an order and a position is fixed from the
// instrument's asset_class when the row is written.
func TestBucketIsFixedFromAssetClassWhenWritten(t *testing.T) {
	for _, tc := range []struct{ assetClass, wantBucket string }{
		{domain.AssetClassStock, domain.BucketStock},
		{domain.AssetClassBondETF, domain.BucketBondETF},
	} {
		t.Run(tc.assetClass, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			instID := insertInstrument(t, db, "7203", "x")
			if _, err := db.ExecContext(ctx, `UPDATE instruments SET asset_class = ? WHERE id = ?`, tc.assetClass, instID); err != nil {
				t.Fatalf("set asset_class: %v", err)
			}
			now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

			sig, err := trading.NewSignalRepository(db).Insert(ctx, domain.TradeSignal{
				InstrumentID: instID, Symbol: "7203", Timestamp: now, Direction: domain.JevDirectionLong, PolicyVersion: "v1",
			})
			if err != nil {
				t.Fatalf("insert signal: %v", err)
			}
			orders := trading.NewOrderRepository(db)
			order, err := orders.Insert(ctx, domain.PaperOrder{
				InstrumentID: instID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
				Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
			})
			if err != nil {
				t.Fatalf("insert order: %v", err)
			}
			pos, err := trading.NewPositionRepository(db).Open(ctx, domain.Position{
				InstrumentID: instID, EntryOrderID: order.ID, Symbol: "7203", Side: domain.PositionSideLong,
				Quantity: 100, EntryPrice: 1000, CurrentPrice: 1000, OpenedAt: now,
			})
			if err != nil {
				t.Fatalf("open position: %v", err)
			}

			for table, id := range map[string]int64{"trade_signals": sig.ID, "paper_orders": order.ID, "positions": pos.ID} {
				if got := bucketOf(t, db, table, id); got != tc.wantBucket {
					t.Errorf("%s.bucket = %q, want %q", table, got, tc.wantBucket)
				}
			}

			// A later change of the asset class does not rewrite the past rows.
			if _, err := db.ExecContext(ctx, `UPDATE instruments SET asset_class = 'bond_etf' WHERE id = ?`, instID); err != nil {
				t.Fatalf("change asset_class: %v", err)
			}
			if got := bucketOf(t, db, "positions", pos.ID); got != tc.wantBucket {
				t.Errorf("positions.bucket after the asset class changed = %q, want %q kept", got, tc.wantBucket)
			}
		})
	}
}

func TestListOpenByBucket_ReturnsOnlyOpenPositionsOfThatBucket(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	stockID := insertInstrument(t, db, "7203", "x")
	bondID := insertInstrument(t, db, "2510", "y")
	if _, err := db.ExecContext(ctx, `UPDATE instruments SET asset_class = 'bond_etf' WHERE id = ?`, bondID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	orders, positions := trading.NewOrderRepository(db), trading.NewPositionRepository(db)
	open := func(instID int64, symbol string, qty int64, entry, current float64) domain.Position {
		t.Helper()
		o, err := orders.Insert(ctx, domain.PaperOrder{InstrumentID: instID, Symbol: symbol, Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: qty, Status: domain.OrderStatusFilled, SubmittedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		p, err := positions.Open(ctx, domain.Position{InstrumentID: instID, EntryOrderID: o.ID, Symbol: symbol, Side: domain.PositionSideLong, Quantity: qty, EntryPrice: entry, CurrentPrice: current, OpenedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	open(stockID, "7203", 100, 2000, 2500)
	open(bondID, "2510", 10, 1000, 1000)

	for bucket, want := range map[string][]string{domain.BucketStock: {"7203"}, domain.BucketBondETF: {"2510"}} {
		got, err := positions.ListOpenByBucket(ctx, bucket)
		if err != nil || len(got) != 1 || got[0].Symbol != want[0] {
			t.Errorf("ListOpenByBucket(%s) = %+v, %v; want only %s", bucket, got, err, want[0])
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE positions SET closed_at = ? WHERE bucket = 'stock'`, "2026-09-27T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got, err := positions.ListOpenByBucket(ctx, domain.BucketStock); err != nil || len(got) != 0 {
		t.Errorf("ListOpenByBucket(stock) after the close = %+v, %v; want none", got, err)
	}
}

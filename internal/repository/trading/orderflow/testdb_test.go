package orderflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// newTestDB opens a fresh, fully migrated SQLite database in a temporary
// directory for a single test, closing it on cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return conn
}

// insertInstrument inserts a minimal instruments row directly (this package
// must not import the market package) and returns its ID, so the tests can
// satisfy the instrument_id foreign keys.
func insertInstrument(t *testing.T, db *sql.DB, symbol, name string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, ?, 'TSE Prime', 1)`, symbol, name)
	if err != nil {
		t.Fatalf("insert instrument fixture %q: %v", symbol, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument fixture id: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func openTestPositionRepo(t *testing.T) (*trading.PositionRepository, *trading.OrderRepository, int64) {
	t.Helper()
	db := newTestDB(t)
	instID := insertInstrument(t, db, "7203", "トヨタ自動車")
	return trading.NewPositionRepository(db), trading.NewOrderRepository(db), instID
}

func insertFilledEntryOrder(t *testing.T, orders *trading.OrderRepository, instrumentID int64, now time.Time) domain.PaperOrder {
	t.Helper()
	ctx := context.Background()
	created, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert entry order fixture: %v", err)
	}
	filled, err := orders.Fill(ctx, created.ID, 2100.0, 0, nil, now)
	if err != nil {
		t.Fatalf("fill entry order fixture: %v", err)
	}
	return filled
}

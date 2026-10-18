package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

var jst = time.FixedZone("JST", 9*60*60)

// seedDB builds a migrated database with two closed Paper positions on
// 2026-10-14 (-6,000 yen, then -2,000 yen) and returns its path.
func seedDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pitha.db")
	db, instID := openSeedDB(t, path)
	defer func() { _ = db.Close() }()
	addPosition(t, db, instID, time.Date(2026, 10, 14, 9, 5, 0, 0, jst), time.Date(2026, 10, 14, 9, 30, 0, 0, jst), -6000)
	addPosition(t, db, instID, time.Date(2026, 10, 14, 10, 0, 0, 0, jst), time.Date(2026, 10, 14, 10, 30, 0, 0, jst), -2000)
	return path
}

func openSeedDB(t *testing.T, path string) (*sql.DB, int64) {
	t.Helper()
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	var instID int64
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM instruments WHERE symbol = '7203'`).Scan(&instID); err == nil {
		return db, instID
	}
	res, err := db.ExecContext(context.Background(), `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'トヨタ', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instID, _ = res.LastInsertId()
	return db, instID
}

// addPosition records a Paper position opened at open and closed at closeAt
// with realized P&L pnl; a zero closeAt leaves it open (an instrument can hold
// one open position, so that must be the last one added).
func addPosition(t *testing.T, db *sql.DB, instID int64, open, closeAt time.Time, pnl float64) {
	t.Helper()
	ctx := context.Background()
	positions, orders := trading.NewPositionRepository(db), trading.NewOrderRepository(db)
	entry, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: open,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	entry, err = orders.Fill(ctx, entry.ID, 2100, 0, nil, open)
	if err != nil {
		t.Fatalf("fill entry order: %v", err)
	}
	pos, err := positions.Open(ctx, domain.Position{
		InstrumentID: instID, EntryOrderID: entry.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: open,
	})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	if closeAt.IsZero() {
		return
	}
	exit, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instID, Symbol: "7203", Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: closeAt,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	if _, err := positions.Close(ctx, pos.ID, exit.ID, 2040, pnl, domain.ExitReasonStopLoss, closeAt); err != nil {
		t.Fatalf("close position: %v", err)
	}
}

func replay(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, &out, &errOut)
	return out.String(), err
}

func TestRun_ThresholdFlags_ReplayTheRecordedDay(t *testing.T) {
	db := seedDB(t)
	out, err := replay(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-loss-jpy", "5000")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"損失上限 5,000 円", "2026-10-14", "daily_loss_limit", "-8,000", "-6,000", "-2,000", "1 halted days"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRun_CapitalAndPercentFlagsDeriveYen(t *testing.T) {
	db := seedDB(t)
	out, err := replay(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-capital", "500000", "-loss-pct", "1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "損失上限 5,000 円") {
		t.Errorf("-capital 500000 -loss-pct 1 should derive 5,000 yen:\n%s", out)
	}
}

func TestRun_NoThresholdFlags_ReadsStoredRuntimeSettings(t *testing.T) {
	db := seedDB(t)
	conn, err := sqlitedb.Open(db)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := system.NewRuntimeSettingsRepository(conn).Set(context.Background(), dailyguardsettings.KeyMaxTrades, "1", time.Now()); err != nil {
		t.Fatalf("set max trades: %v", err)
	}
	_ = conn.Close()

	out, err := replay(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "最大取引回数 1 回") || !strings.Contains(out, "損失上限 無効") {
		t.Errorf("stored max_daily_trades=1 should be the only rule:\n%s", out)
	}
}

func TestRun_IsReadOnly(t *testing.T) {
	db := seedDB(t)
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-loss-jpy", "5000"); err != nil {
		t.Fatalf("run: %v", err)
	}
	after, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the database file changed")
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := replay(t, "-db", missing, "-from", "2026-10-14", "-to", "2026-10-14"); err == nil {
		t.Error("a missing database should be an error")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a missing database must not be created")
	}
}

func TestRun_RejectsBadFlags(t *testing.T) {
	db := seedDB(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no db", []string{"-from", "2026-10-14", "-to", "2026-10-14"}},
		{"no range", []string{"-db", db}},
		{"bad date", []string{"-db", db, "-from", "10/14", "-to", "2026-10-14"}},
		{"reversed range", []string{"-db", db, "-from", "2026-10-15", "-to", "2026-10-14"}},
		{"negative threshold", []string{"-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-loss-jpy", "-1"}},
		{"percent above 100", []string{"-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-giveback-pct", "101"}},
		{"not a number", []string{"-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-max-trades", "many"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := replay(t, tc.args...); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestRun_RangeExcludesOtherDays(t *testing.T) {
	db := seedDB(t)
	out, err := replay(t, "-db", db, "-from", "2026-10-15", "-to", "2026-10-16", "-loss-jpy", "5000")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "2026-10-14") || !strings.Contains(out, "0 halted days") {
		t.Errorf("an empty range should list no day:\n%s", out)
	}
}

// Positions that cross the range edge are still replayed: one opened before
// -from adds its P&L to the day it closed in, one opened in the range but
// closed after -to (or still open) counts as an opened trade without P&L, and
// neither adds a day to the output.
func TestRun_OvernightAndOpenPositionsAtTheRangeEdges(t *testing.T) {
	path := seedDB(t)
	db, instID := openSeedDB(t, path)
	addPosition(t, db, instID, time.Date(2026, 10, 13, 14, 0, 0, 0, jst), time.Date(2026, 10, 14, 9, 10, 0, 0, jst), 1000)  // opened before -from
	addPosition(t, db, instID, time.Date(2026, 10, 14, 11, 0, 0, 0, jst), time.Date(2026, 10, 15, 9, 30, 0, 0, jst), -9999) // closed after -to
	addPosition(t, db, instID, time.Date(2026, 10, 14, 14, 0, 0, 0, jst), time.Time{}, 0)                                   // still open
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// No rule can refuse anything: 4 positions opened on the 14th, and only
	// the closes of that day have P&L (+1,000 -6,000 -2,000).
	out, err := replay(t, "-db", path, "-from", "2026-10-14", "-to", "2026-10-14", "-max-trades", "99")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"2026-10-14", "4/4/0", "-7,000"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"2026-10-13", "2026-10-15", "9,999"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output must not contain %q:\n%s", unwanted, out)
		}
	}

	// Loss limit 5,000: the overnight +1,000 and the -6,000 reach -5,000, so
	// the later opens (including the still-open one) are refused.
	out, err = replay(t, "-db", path, "-from", "2026-10-14", "-to", "2026-10-14", "-loss-jpy", "5000")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"4/1/3", "-7,000", "-5,000", "-2,000", "1 halted days"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

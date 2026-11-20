package researchfx_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func newRepo(t *testing.T) (*researchfx.Repository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return researchfx.NewRepository(db), db
}

func candle(day string, minute int, bid, ask *rfx.OHLC) rfx.Candle {
	start, err := rfx.DayStart(day)
	if err != nil {
		panic(err)
	}
	return rfx.Candle{Symbol: "USD_JPY", OpenTime: start.Add(time.Duration(minute) * time.Minute).UTC(), TradingDay: day, Bid: bid, Ask: ask}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecordDay_WritesMergedCandlesAndTheMarkerAtomicallyAndIdempotently(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	px := func(v float64) *rfx.OHLC { return &rfx.OHLC{Open: v, High: v + 1, Low: v - 1, Close: v + 0.5} }
	candles := []rfx.Candle{candle("2026-10-15", 0, px(150), px(150.01)), candle("2026-10-15", 1, px(151), nil), candle("2026-10-15", 3, nil, px(152))}
	at := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)

	d, err := repo.RecordDay(ctx, "USD_JPY", "2026-10-15", candles, at)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != rfx.StatusRecorded || d.CandleCount != 3 {
		t.Fatalf("marker = %+v, want recorded with 3 candles", d)
	}
	// The first candle has both sides, the second only BID, the third only ASK.
	var bidOpen, askOpen sql.NullFloat64
	var openTime string
	for i, want := range [][2]bool{{true, true}, {true, false}, {false, true}} {
		if err := db.QueryRow(`SELECT open_time, bid_open, ask_open FROM research_fx_candles ORDER BY open_time LIMIT 1 OFFSET ?`, i).Scan(&openTime, &bidOpen, &askOpen); err != nil {
			t.Fatal(err)
		}
		if bidOpen.Valid != want[0] || askOpen.Valid != want[1] {
			t.Errorf("candle %d sides = bid %v ask %v, want %v", i, bidOpen.Valid, askOpen.Valid, want)
		}
	}
	if openTime != "2026-10-14T21:03:00.000000000Z" {
		t.Errorf("last open_time = %q, want UTC with 9 fractional digits", openTime)
	}

	// A rerun (even with other numbers) adds no row and keeps the stored candles.
	changed := []rfx.Candle{candle("2026-10-15", 0, px(999), px(999))}
	if d, err = repo.RecordDay(ctx, "USD_JPY", "2026-10-15", changed, at.Add(time.Hour)); err != nil || d.CandleCount != 3 {
		t.Fatalf("rerun = %+v, %v; want 3 candles still", d, err)
	}
	var kept float64
	if err := db.QueryRow(`SELECT bid_open FROM research_fx_candles ORDER BY open_time LIMIT 1`).Scan(&kept); err != nil || kept != 150 {
		t.Fatalf("the stored candle was overwritten: %v, %v", kept, err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM research_fx_days`); got != 1 {
		t.Fatalf("markers = %d, want 1 (replaced, not duplicated)", got)
	}

	// A day without a candle is an 'empty' marker (market closed), not an error and not a gap.
	if d, err = repo.RecordDay(ctx, "USD_JPY", "2026-10-17", nil, at); err != nil || d.Status != rfx.StatusEmpty || d.CandleCount != 0 {
		t.Fatalf("empty day = %+v, %v", d, err)
	}
	marked, err := repo.MarkedDays(ctx, "USD_JPY", "2026-10-01", "2026-10-31")
	if err != nil || len(marked) != 2 || !marked["2026-10-15"] || !marked["2026-10-17"] {
		t.Fatalf("MarkedDays = %v, %v", marked, err)
	}
	if marked, _ = repo.MarkedDays(ctx, "USD_JPY", "2026-10-16", "2026-10-16"); len(marked) != 0 {
		t.Fatalf("MarkedDays of an unmarked day = %v", marked)
	}
	if marked, _ = repo.MarkedDays(ctx, "EUR_USD", "2026-10-01", "2026-10-31"); len(marked) != 0 {
		t.Fatalf("MarkedDays of another pair = %v", marked)
	}
}

// A failure while writing rolls the whole day back: no candle without its marker.
func TestRecordDay_ARejectedCandleLeavesNothingBehind(t *testing.T) {
	repo, db := newRepo(t)
	bad := &rfx.OHLC{Open: 0, High: 1, Low: 0, Close: 1} // a non-positive price violates the table's CHECK
	good := &rfx.OHLC{Open: 1, High: 1, Low: 1, Close: 1}
	_, err := repo.RecordDay(context.Background(), "USD_JPY", "2026-10-15", []rfx.Candle{candle("2026-10-15", 0, good, good), candle("2026-10-15", 1, bad, good)}, time.Now())
	if err == nil {
		t.Fatal("a non-positive price was accepted")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_fx_candles`) + count(t, db, `SELECT COUNT(*) FROM research_fx_days`); n != 0 {
		t.Fatalf("%d rows left after the failed write", n)
	}
	// A candle that belongs to another day or pair is refused before it is written.
	good2 := candle("2026-10-14", 0, good, good)
	if _, err := repo.RecordDay(context.Background(), "USD_JPY", "2026-10-15", []rfx.Candle{good2}, time.Now()); err == nil {
		t.Fatal("a candle of another trading day was accepted")
	}
	if _, err := repo.RecordDay(context.Background(), "USD_JPY", "15/10/2026", nil, time.Now()); err == nil {
		t.Fatal("a malformed trading day was accepted")
	}
}

func TestTradingDay_RollsOverAt0600JST(t *testing.T) {
	jst := rfx.JST
	for _, tt := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 15, 5, 59, 59, 0, jst), "2026-10-14"},
		{time.Date(2026, 10, 15, 6, 0, 0, 0, jst), "2026-10-15"},
		{time.Date(2026, 10, 15, 23, 0, 0, 0, jst), "2026-10-15"},
		{time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC), "2026-10-15"}, // 06:00 JST
	} {
		if got := rfx.TradingDay(tt.at); got != tt.want {
			t.Errorf("TradingDay(%v) = %s, want %s", tt.at, got, tt.want)
		}
	}
	end, err := rfx.DayEnd("2026-10-15")
	if err != nil || !end.Equal(time.Date(2026, 10, 16, 6, 0, 0, 0, jst)) {
		t.Errorf("DayEnd = %v, %v", end, err)
	}
}

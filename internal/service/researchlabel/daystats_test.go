package researchlabel

import (
	"context"
	"math"
	"testing"
)

// The day mark and the day sums the MCP tool aggregates from are what the day's rows add up to.
func TestRecorder_DayMarkAndSumsMatchTheRows(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	if _, err := newRecorder(db, "2026-10-14 16:30").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var err error
	// The day mark keeps the day's counts, and the day sums are what the rows add up to: per day,
	// instrument 1 has both sides (62 / 32 / 85 rows by horizon), instrument 2 only SHORT.
	var snaps, l30, l60, lcl int
	if err := db.QueryRow(`SELECT snapshots, labeled_30m, labeled_60m, labeled_close FROM research_label_days WHERE day = '2026-10-13'`).Scan(&snaps, &l30, &l60, &lcl); err != nil {
		t.Fatal(err)
	}
	if snaps != 180 || l30 != 2*62 || l60 != 2*32 || lcl != 2*85 {
		t.Errorf("day mark = snapshots %d, 30m %d, 60m %d, close %d; want 180, 124, 64, 170", snaps, l30, l60, lcl)
	}
	var rowsN, wins, spreadRows, above int
	var sumNet, sumCost, sumSpread, sumLess float64
	err = db.QueryRow(`SELECT row_count, wins, spread_rows, spread_above_cost, sum_net, sum_cost, sum_spread, sum_gross_less_half_spread
		FROM research_label_day_stats WHERE day = '2026-10-13' AND horizon = '30m' AND side = 'LONG'`).Scan(&rowsN, &wins, &spreadRows, &above, &sumNet, &sumCost, &sumSpread, &sumLess)
	if err != nil {
		t.Fatal(err)
	}
	// LONG 30m of instrument 1: entry ask p+1 at minute i (i = 0..61), exit price p+30: gross = 29 / (p+1)... by hand:
	// every row wins (gross about 290 bps, cost 19), the cost is 19 per row, the spread 20 exceeds the cost 19 on every row.
	var wantGross float64
	for i := 0; i < 62; i++ {
		// The exit is the bar 30 minutes on; for the last two entries (15:00, 15:01) the target is 15:30 / 15:31 and the
		// last bar of the day, 15:29 (price 1089), is the exit within the tolerance.
		exit := math.Min(1030+float64(i), 1089)
		wantGross += (exit - (1001 + float64(i))) / (1001 + float64(i)) * 10000
	}
	if rowsN != 62 || wins != 62 || spreadRows != 62 || above != 62 {
		t.Errorf("LONG 30m sums rows/wins/spread/above = %d/%d/%d/%d, want 62 each", rowsN, wins, spreadRows, above)
	}
	near(t, "sum_cost", sumCost, 62*19)
	near(t, "sum_spread", sumSpread, 62*20)
	near(t, "sum_net", sumNet, wantGross-62*19)
	near(t, "sum_gross_less_half_spread", sumLess, wantGross-62*10)
	// SHORT 30m: both instruments, no row wins (the price rises), cost 27 exceeds the spread 20 on every row.
	err = db.QueryRow(`SELECT row_count, wins, spread_above_cost FROM research_label_day_stats WHERE day = '2026-10-13' AND horizon = '30m' AND side = 'SHORT'`).Scan(&rowsN, &wins, &above)
	if err != nil || rowsN != 124 || wins != 0 || above != 0 {
		t.Errorf("SHORT 30m sums = rows %d wins %d above %d, %v; want 124 / 0 / 0", rowsN, wins, above, err)
	}
	// Instrument 2 has no ask: the LONG side has no 'close' rows beyond instrument 1's 85.
	if err := db.QueryRow(`SELECT row_count FROM research_label_day_stats WHERE day = '2026-10-13' AND horizon = 'close' AND side = 'LONG'`).Scan(&rowsN); err != nil || rowsN != 85 {
		t.Errorf("LONG close rows = %d, %v; want 85 (instrument 1 only)", rowsN, err)
	}
}

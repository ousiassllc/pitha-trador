package researchlabel

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
)

// day is a TSE trading day (Wednesday, 2026-10-14).
const day = "2026-10-14"

var testJST = time.FixedZone("JST", 9*60*60)

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, testJST)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func fptr(v float64) *float64 { return &v }

// bar is a quoted bar; the quote is price -/+ 1 and spread_bps is given.
func bar(id int64, hhmm string, price float64) domain.Snapshot {
	return domain.Snapshot{ID: id, InstrumentID: 7, Symbol: "7203", Timestamp: at(hhmm), Price: price,
		Bid: fptr(price - 1), Ask: fptr(price + 1), SpreadBps: fptr(20)}
}

// minutes returns one bar per minute from..to inclusive, priced by price(i)
// with i the minute index; ids count from 1.
func minutes(from, to string, price func(i int) float64) []domain.Snapshot {
	var out []domain.Snapshot
	for t, i := at(from), 0; !t.After(at(to)); t, i = t.Add(time.Minute), i+1 {
		out = append(out, domain.Snapshot{ID: int64(i + 1), InstrumentID: 7, Symbol: "7203", Timestamp: t, Price: price(i),
			Bid: fptr(price(i) - 1), Ask: fptr(price(i) + 1), SpreadBps: fptr(20)})
	}
	return out
}

func flat(p float64) func(int) float64 { return func(int) float64 { return p } }

func find(rows []rlabel.Label, id int64, horizon string) *rlabel.Label {
	for i := range rows {
		if rows[i].SnapshotID == id && rows[i].Horizon == horizon {
			return &rows[i]
		}
	}
	return nil
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.12f, want %.12f", name, got, want)
	}
}

func horizonsOf(rows []rlabel.Label, id int64) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		if r.SnapshotID == id {
			out[r.Horizon] = true
		}
	}
	return out
}

func TestLabelBars_LongAndShortGrossAndNetByHand(t *testing.T) {
	// Entry 10:00: bid 999, ask 1001, price 1000. 30 minutes later (10:30) the
	// price is 1010. LONG buys at the ask 1001, SHORT sells at the bid 999.
	bars := []domain.Snapshot{bar(1, "10:00", 1000), bar(2, "10:30", 1010)}
	costs := Costs{Long: 19, Short: 27}
	rows := LabelBars(bars, costs)
	got := find(rows, 1, rlabel.Horizon30m)
	if got == nil {
		t.Fatalf("no 30m row for the entry; rows %+v", rows)
	}
	wantLong := (1010.0 - 1001.0) / 1001.0 * 10000 // +89.91 bps
	wantShort := -(1010.0 - 999.0) / 999.0 * 10000 // -110.11 bps
	near(t, "long gross", got.Long.GrossBps, wantLong)
	near(t, "long cost", got.Long.CostBps, 19)
	near(t, "long net", got.Long.NetBps, wantLong-19)
	near(t, "short gross", got.Short.GrossBps, wantShort)
	near(t, "short cost", got.Short.CostBps, 27)
	near(t, "short net", got.Short.NetBps, wantShort-27)
	if !got.ExitAt.Equal(at("10:30")) || got.ExitPrice != 1010 || got.EntryPrice != 1000 || *got.SpreadBps != 20 {
		t.Errorf("row identity = %+v", got)
	}
	// A falling price: SHORT gains, LONG loses.
	rows = LabelBars([]domain.Snapshot{bar(1, "10:00", 1000), bar(2, "10:30", 990)}, costs)
	got = find(rows, 1, rlabel.Horizon30m)
	near(t, "short gross after a fall", got.Short.GrossBps, (999.0-990.0)/999.0*10000)
	near(t, "long gross after a fall", got.Long.GrossBps, (990.0-1001.0)/1001.0*10000)
	// The 10:30 bar is itself an entry but has no bar 30 minutes later.
	if find(rows, 2, rlabel.Horizon30m) != nil {
		t.Error("the last bar of the day got a 30m row")
	}
}

func TestLabelBars_OneSidedQuoteLeavesTheOtherSideNull(t *testing.T) {
	noAsk := bar(1, "10:00", 1000)
	noAsk.Ask = nil
	noBid := bar(2, "10:01", 1000)
	noBid.Bid = fptr(0) // a zero quote is not executable
	neither := bar(3, "10:02", 1000)
	neither.Bid, neither.Ask = nil, nil
	rows := LabelBars([]domain.Snapshot{noAsk, noBid, neither, bar(4, "10:30", 1010), bar(5, "10:31", 1010), bar(6, "10:32", 1010)}, Costs{Long: 10, Short: 10})

	got := find(rows, 1, rlabel.Horizon30m)
	if got == nil || got.Long != nil || got.Short == nil {
		t.Fatalf("no ask: row %+v, want only the SHORT side", got)
	}
	near(t, "short gross", got.Short.GrossBps, -(1010.0-999.0)/999.0*10000)
	got = find(rows, 2, rlabel.Horizon30m)
	if got == nil || got.Short != nil || got.Long == nil {
		t.Fatalf("no bid: row %+v, want only the LONG side", got)
	}
	if len(horizonsOf(rows, 3)) != 0 {
		t.Errorf("a bar with neither quote got rows %v", horizonsOf(rows, 3))
	}
}

func TestLabelBars_FixedHorizonUsesTheLastBarAtOrBeforeTheTargetWithinTolerance(t *testing.T) {
	entry := bar(1, "10:00", 1000)
	for name, tc := range map[string]struct {
		exit string // the last bar at or before 10:30
		want bool
	}{
		"exactly at the target":     {"10:30", true},
		"one minute short":          {"10:29", true},
		"exactly the tolerance":     {"10:28", true},
		"just beyond the tolerance": {"10:27", false},
	} {
		bars := []domain.Snapshot{entry, bar(2, tc.exit, 1005), bar(3, "10:31", 2000)} // a later bar never replaces the exit
		got := find(LabelBars(bars, Costs{}), 1, rlabel.Horizon30m)
		if (got != nil) != tc.want {
			t.Errorf("%s: row = %v, want present=%v", name, got, tc.want)
			continue
		}
		if got != nil && (got.ExitPrice != 1005 || !got.ExitAt.Equal(at(tc.exit))) {
			t.Errorf("%s: exit = %v at %v, want the last bar at or before the target", name, got.ExitPrice, got.ExitAt)
		}
	}
	// Sixty minutes: same rule with a 60-minute target.
	bars := []domain.Snapshot{entry, bar(2, "11:00", 1003)} // 11:00 is 60 minutes after 10:00
	if find(LabelBars(bars, Costs{}), 1, rlabel.Horizon60m) == nil {
		t.Error("no 60m row for a bar exactly 60 minutes later")
	}
	if find(LabelBars(bars, Costs{}), 1, rlabel.Horizon30m) != nil {
		t.Error("a 30m row from a bar 60 minutes later (outside the tolerance)")
	}
}

func TestLabelBars_Empty(t *testing.T) {
	if rows := LabelBars(nil, Costs{}); len(rows) != 0 {
		t.Errorf("rows for no bars: %v", rows)
	}
}

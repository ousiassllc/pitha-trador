package researchlabel

import (
	"testing"
	"time"

	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
)

func TestLabelBars_AWindowCrossingTheLunchBreakIsNeverShortened(t *testing.T) {
	// A full morning session: bars every minute 09:00-11:29, then the afternoon.
	bars := append(minutes("09:00", "11:29", flat(1000)), minutes("12:30", "15:29", flat(1000))...)
	for i := range bars {
		bars[i].ID = int64(i + 1)
	}
	idOf := func(hhmm string) int64 {
		for _, b := range bars {
			if b.Timestamp.Equal(at(hhmm)) {
				return b.ID
			}
		}
		t.Fatalf("no bar at %s", hhmm)
		return 0
	}
	rows := LabelBars(bars, Costs{})
	// 10:45 + 30m = 11:15 (inside the morning): labeled. 10:45 + 60m = 11:45 is in
	// the lunch break; the last bar before it is 11:29, 14 minutes short.
	if find(rows, idOf("10:45"), rlabel.Horizon30m) == nil {
		t.Error("10:45 has no 30m row")
	}
	if find(rows, idOf("10:45"), rlabel.Horizon60m) != nil {
		t.Error("10:45 got a 60m row whose window crosses the lunch break (shortened)")
	}
	// 11:10 + 30m = 11:40: also across the break.
	if find(rows, idOf("11:10"), rlabel.Horizon30m) != nil {
		t.Error("11:10 got a 30m row across the lunch break")
	}
	// 11:00 + 30m = 11:30: the last morning bar 11:29 is within tolerance, as in the live label.
	if find(rows, idOf("11:00"), rlabel.Horizon30m) == nil {
		t.Error("11:00 has no 30m row though the bar one minute short exists")
	}
	// 12:30 + 30m is a normal afternoon window.
	if find(rows, idOf("12:30"), rlabel.Horizon30m) == nil {
		t.Error("12:30 has no 30m row")
	}
}

func TestLabelBars_SixtyMinutesNearTheCloseHasNoRow(t *testing.T) {
	bars := minutes("14:00", "15:29", flat(1000))
	rows := LabelBars(bars, Costs{})
	idOf := func(hhmm string) int64 {
		for _, b := range bars {
			if b.Timestamp.Equal(at(hhmm)) {
				return b.ID
			}
		}
		t.Fatalf("no bar at %s", hhmm)
		return 0
	}
	if find(rows, idOf("14:40"), rlabel.Horizon60m) != nil {
		t.Error("14:40 + 60m is past the close, but got a row")
	}
	if find(rows, idOf("14:15"), rlabel.Horizon60m) == nil {
		t.Error("14:15 + 60m = 15:15 should be labeled")
	}
	if find(rows, idOf("14:35"), rlabel.Horizon30m) == nil {
		t.Error("14:35 + 30m = 15:05 should be labeled")
	}
}

func TestLabelBars_CloseExitRule(t *testing.T) {
	t.Run("the last bar at or after 15:25 is the exit", func(t *testing.T) {
		bars := minutes("14:00", "15:26", func(i int) float64 { return 1000 + float64(i) })
		rows := LabelBars(bars, Costs{Long: 10, Short: 10})
		last := bars[len(bars)-1]
		got := find(rows, bars[0].ID, rlabel.HorizonClose)
		if got == nil {
			t.Fatal("no close row for a day ending 15:26")
		}
		if !got.ExitAt.Equal(last.Timestamp) || got.ExitPrice != last.Price {
			t.Errorf("close exit = %v @ %v, want the last bar %v @ %v", got.ExitPrice, got.ExitAt, last.Price, last.Timestamp)
		}
		near(t, "long gross", got.Long.GrossBps, (last.Price-1001)/1001*10000)
	})
	t.Run("a bar after 15:30 is not the exit", func(t *testing.T) {
		bars := append(minutes("14:00", "15:30", flat(1000)), bar(999, "15:35", 5000))
		got := find(LabelBars(bars, Costs{}), bars[0].ID, rlabel.HorizonClose)
		if got == nil || got.ExitPrice != 1000 || !got.ExitAt.Equal(at("15:30")) {
			t.Errorf("close row = %+v, want the 15:30 bar", got)
		}
	})
	t.Run("a day ending at 15:10 has no close row", func(t *testing.T) {
		rows := LabelBars(minutes("14:00", "15:10", flat(1000)), Costs{})
		for _, r := range rows {
			if r.Horizon == rlabel.HorizonClose {
				t.Fatalf("close row for a day whose last bar is 15:10: %+v", r)
			}
		}
	})
	t.Run("15:24 is the limit of the accepted bar", func(t *testing.T) {
		rows := LabelBars(minutes("14:00", "15:24", flat(1000)), Costs{})
		for _, r := range rows {
			if r.Horizon == rlabel.HorizonClose {
				t.Fatalf("close row for a day whose last bar is 15:24: %+v", r)
			}
		}
	})
	t.Run("a snapshot at or after 15:25 has no close row", func(t *testing.T) {
		bars := minutes("15:00", "15:29", flat(1000))
		rows := LabelBars(bars, Costs{})
		for _, r := range rows {
			if r.Horizon == rlabel.HorizonClose && !r.SnapshotAt.Before(at("15:25")) {
				t.Fatalf("close row for the snapshot at %v", r.SnapshotAt)
			}
		}
		if find(rows, bars[0].ID, rlabel.HorizonClose) == nil {
			t.Error("15:00 has no close row")
		}
		var at1525 int64
		for _, b := range bars {
			if b.Timestamp.Equal(at("15:25")) {
				at1525 = b.ID
			}
		}
		if len(horizonsOf(rows, at1525)) != 0 {
			t.Errorf("15:25 got rows %v", horizonsOf(rows, at1525))
		}
	})
	t.Run("a non-trading day has no close row", func(t *testing.T) {
		sat := func(hhmm string) time.Time {
			ts, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-17 "+hhmm, testJST)
			return ts.UTC()
		}
		bars := minutes("14:00", "15:29", flat(1000))
		for i := range bars {
			bars[i].Timestamp = sat(bars[i].Timestamp.In(testJST).Format("15:04"))
		}
		for _, r := range LabelBars(bars, Costs{}) {
			if r.Horizon == rlabel.HorizonClose {
				t.Fatalf("close row on a Saturday: %+v", r)
			}
		}
	})
}

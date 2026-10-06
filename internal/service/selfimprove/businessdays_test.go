package selfimprove

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jst(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, marketcalendar.JST)
}

// tradingDaysBetween counts TSE trading days in (from, to].
func tradingDaysBetween(from, to time.Time) int {
	n := 0
	for d := from.In(marketcalendar.JST).AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if marketcalendar.TSE.IsTradingDay(d) {
			n++
		}
	}
	return n
}

// Issue #600: the lookback skips holidays, so a 20営業日 window is 20
// trading days even across Golden Week (2026-05-08 reaches back to
// 2026-04-06: 4/29, 5/4, 5/5, 5/6 are closed; 5/3 is a Sunday).
func TestBusinessDaysBefore_SkipsExchangeHolidays(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"golden week", jst(2026, time.May, 8), jst(2026, time.April, 6)},
		{"year end", jst(2027, time.January, 8), jst(2026, time.December, 9)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := businessDaysBefore(tc.now, 20)
			if got := tradingDaysBetween(got, tc.now); got != 20 {
				t.Errorf("trading days in the lookback = %d, want 20", got)
			}
			if !got.Equal(tc.want) {
				t.Errorf("businessDaysBefore(%s, 20) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

func TestBusinessDaysAfter_SkipsWeekendsHolidaysAndYearEnd(t *testing.T) {
	cases := []struct {
		name  string
		from  time.Time
		days  int
		want  time.Time
		label string
	}{
		{"weekend", jst(2026, time.January, 9), 1, jst(2026, time.January, 13), "Fri -> Tue (1/12 成人の日)"},
		{"golden week", jst(2026, time.May, 1), 5, jst(2026, time.May, 13), "5/4-5/6 closed"},
		{"year end", jst(2026, time.December, 30), 1, jst(2027, time.January, 4), "12/31-1/3 closed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := businessDaysAfter(tc.from, tc.days)
			if !got.Equal(tc.want) {
				t.Errorf("businessDaysAfter(%s, %d) = %s, want %s (%s)", tc.from, tc.days, got, tc.want, tc.label)
			}
		})
	}
}

// The calendar day is judged in JST: 2026-05-07 20:00 UTC is already Fri
// 5/8 05:00 JST, and a Sunday-in-UTC instant that is Monday in JST counts.
func TestBusinessDays_JudgesJSTDate(t *testing.T) {
	friJST := time.Date(2026, time.May, 7, 20, 0, 0, 0, time.UTC) // Fri 5/8 05:00 JST
	got := businessDaysBefore(friJST, 1)
	if want := jst(2026, time.May, 7); got.Day() != want.Day() || got.Month() != want.Month() {
		t.Errorf("businessDaysBefore(%s, 1) = %s, want JST date 5/7", friJST, got)
	}
}

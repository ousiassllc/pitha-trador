package selfimprove

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// businessDaysBefore/businessDaysAfter count "N営業日" (FR-SELFIMPROVE-4's
// lookback, FR-SELFIMPROVE-6's tracking window) as TSE trading days
// (marketcalendar.TSE.IsTradingDay: weekends, national holidays, 振替休日,
// 国民の休日 and the 12/31〜1/3 年末年始休場 are skipped), judged on the JST
// date. The returned time is t shifted by whole JST days, in JST.
func businessDaysBefore(t time.Time, days int) time.Time {
	return shiftBusinessDays(t, days, -1)
}

func businessDaysAfter(t time.Time, days int) time.Time {
	return shiftBusinessDays(t, days, 1)
}

func shiftBusinessDays(t time.Time, days, step int) time.Time {
	d := t.In(marketcalendar.JST)
	for remaining := days; remaining > 0; {
		d = d.AddDate(0, 0, step)
		if marketcalendar.TSE.IsTradingDay(d) {
			remaining--
		}
	}
	return d
}

package tachibanawatch

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const dateLayout = "2006-01-02"

// maxDayScan bounds the walks over the calendar (the longest closure is far
// shorter).
const maxDayScan = 31

func parseDay(day string) time.Time {
	t, err := time.ParseInLocation(dateLayout, day, tachibana.JST)
	if err != nil {
		return time.Time{}
	}
	return t
}

// previousDay is the calendar day before day (YYYY-MM-DD, JST).
func previousDay(day string) string { return dayBefore(day, 1) }

// dayBefore is the calendar day n days before day.
func dayBefore(day string, n int) string {
	return parseDay(day).AddDate(0, 0, -n).Format(dateLayout)
}

// nextTradingDay is the first 立会日 strictly after day: the date a list
// decided on day's night is for.
func nextTradingDay(day string) string {
	t := parseDay(day)
	for range maxDayScan {
		t = t.AddDate(0, 0, 1)
		if marketcalendar.TSE.IsTradingDay(t) {
			break
		}
	}
	return t.Format(dateLayout)
}

// lastTradingDay is the latest 立会日 on or before day: the day whose bars
// the night of day screens.
func lastTradingDay(day string) string {
	t := parseDay(day)
	for range maxDayScan {
		if marketcalendar.TSE.IsTradingDay(t) {
			break
		}
		t = t.AddDate(0, 0, -1)
	}
	return t.Format(dateLayout)
}

// SessionDate is the 立会日 whose list is in use at now: today until the
// close at 15:30, the next 立会日 from then on (the list decided in the
// evening takes over at once) and on non-trading days.
func SessionDate(now time.Time) string {
	now = now.In(tachibana.JST)
	today := now.Format(dateLayout)
	if marketcalendar.TSE.IsTradingDay(now) {
		if closeAt, ok := marketcalendar.TSE.CloseAt(now); ok && now.Before(closeAt) {
			return today
		}
	}
	return nextTradingDay(today)
}

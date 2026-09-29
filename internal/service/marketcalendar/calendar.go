package marketcalendar

import "time"

// JST is Japan Standard Time (UTC+9, no DST). A fixed zone is used so
// the package needs no tzdata on the host.
var JST = time.FixedZone("JST", 9*60*60)

// Session boundaries in minutes after JST midnight. Each session is the
// half-open interval [open, close).
const (
	morningOpen    = 9 * 60
	morningClose   = 11*60 + 30
	afternoonOpen  = 12*60 + 30
	afternoonClose = 15*60 + 30
)

// TSE is the Tokyo Stock Exchange calendar (see package doc).
var TSE = Calendar{}

// Calendar is the TSE trading calendar. The zero value is ready to use.
type Calendar struct{}

// IsTradingDay reports whether the JST date of t is a TSE trading day.
func (Calendar) IsTradingDay(t time.Time) bool {
	t = t.In(JST)
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	if isExchangeClosure(t.Month(), t.Day()) {
		return false
	}
	return !isNationalHoliday(t.Year(), t.Month(), t.Day())
}

// IsOpen reports whether t falls inside a trading session: 9:00-11:30 or
// 12:30-15:30 JST (start inclusive, end exclusive) on a trading day. The
// 11:30-12:30 lunch break is not in session.
func (c Calendar) IsOpen(t time.Time) bool {
	if !c.IsTradingDay(t) {
		return false
	}
	t = t.In(JST)
	m := t.Hour()*60 + t.Minute()
	return (m >= morningOpen && m < morningClose) || (m >= afternoonOpen && m < afternoonClose)
}

// OpenAt returns the 前場 opening time (9:00 JST) of t's JST date, or
// false when that date is not a trading day.
func (c Calendar) OpenAt(t time.Time) (time.Time, bool) {
	return c.atMinute(t, morningOpen)
}

// CloseAt returns the 大引け time (15:30 JST) of t's JST date, or false
// when that date is not a trading day.
func (c Calendar) CloseAt(t time.Time) (time.Time, bool) {
	return c.atMinute(t, afternoonClose)
}

func (c Calendar) atMinute(t time.Time, minutes int) (time.Time, bool) {
	if !c.IsTradingDay(t) {
		return time.Time{}, false
	}
	t = t.In(JST)
	return time.Date(t.Year(), t.Month(), t.Day(), minutes/60, minutes%60, 0, 0, JST), true
}

// isExchangeClosure reports the exchange's own year-end/new-year closure
// (12/31 and 1/1-1/3), on top of national holidays and weekends.
func isExchangeClosure(month time.Month, day int) bool {
	return (month == time.December && day == 31) || (month == time.January && day <= 3)
}

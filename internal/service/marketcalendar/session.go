package marketcalendar

import "time"

// lunchMidpoint splits a JST day into its 前場 half and 後場 half: the middle
// of the 11:30-12:30 lunch break.
const lunchMidpoint = 12 * 60

// SameSession reports whether a and b fall into the same trading session:
// the same JST date and the same side of the lunch break (前場 or 後場).
// Bars of different sessions (前営業日の引け→翌朝の寄り、前場の最終バー→後場
// 寄り) are not consecutive even when no bar is missing between them. It
// classifies by the clock only, so it does not depend on IsOpen: callers
// feed it bars that were produced during a session.
func SameSession(a, b time.Time) bool {
	a, b = a.In(JST), b.In(JST)
	if a.Year() != b.Year() || a.YearDay() != b.YearDay() {
		return false
	}
	return (a.Hour()*60+a.Minute() >= lunchMidpoint) == (b.Hour()*60+b.Minute() >= lunchMidpoint)
}

// SessionStart returns the opening time of the trading session t falls into
// (前場 9:00 or 後場 12:30 JST of t's date), or false when t is not inside a
// session (IsOpen is false: a non-trading day, before 9:00, the lunch break,
// at or after 15:30).
func (c Calendar) SessionStart(t time.Time) (time.Time, bool) {
	if !c.IsOpen(t) {
		return time.Time{}, false
	}
	t = t.In(JST)
	start := morningOpen
	if t.Hour()*60+t.Minute() >= afternoonOpen {
		start = afternoonOpen
	}
	return time.Date(t.Year(), t.Month(), t.Day(), start/60, start%60, 0, 0, JST), true
}

// Session is one trading session of a day: [Open, Close).
type Session struct {
	Open, Close time.Time
}

// Sessions returns the two sessions (前場 09:00-11:30, 後場 12:30-15:30 JST) of
// t's JST date, in order, or nil when that date is not a trading day.
func (c Calendar) Sessions(t time.Time) []Session {
	if !c.IsTradingDay(t) {
		return nil
	}
	t = t.In(JST)
	at := func(minutes int) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), minutes/60, minutes%60, 0, 0, JST)
	}
	return []Session{{Open: at(morningOpen), Close: at(morningClose)}, {Open: at(afternoonOpen), Close: at(afternoonClose)}}
}

// previousTradingDaySearchDays bounds PreviousTradingDay's scan, for the same
// reason as nextOpenSearchDays.
const previousTradingDaySearchDays = 31

// PreviousTradingDay returns 00:00 JST of the last trading day strictly before
// t's JST date, or false when none lies within the scan bound.
func (c Calendar) PreviousTradingDay(t time.Time) (time.Time, bool) {
	t = t.In(JST)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, JST)
	for range previousTradingDaySearchDays {
		day = day.AddDate(0, 0, -1)
		if c.IsTradingDay(day) {
			return day, true
		}
	}
	return time.Time{}, false
}

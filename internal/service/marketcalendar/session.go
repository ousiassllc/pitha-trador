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

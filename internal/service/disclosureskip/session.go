package disclosureskip

import "time"

// The TSE sessions in minutes after JST midnight, as marketcalendar has them
// ([open, close), 前場 and 後場). They are kept here, next to the Calendar
// this package already takes (IsTradingDay only), so the rule's tests can pin
// their own holidays; a test checks them against marketcalendar.TSE.
const (
	morningOpen    = 9 * 60
	morningClose   = 11*60 + 30
	afternoonOpen  = 12*60 + 30
	afternoonClose = 15*60 + 30
)

// span is one continuous trading session, [from, to).
type span struct{ from, to time.Time }

// sessionsOn returns the sessions of the JST date of t: the 前場 and the 後場
// on a trading day, none otherwise.
func sessionsOn(cal Calendar, t time.Time) []span {
	t = t.In(jst)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, jst)
	if !cal.IsTradingDay(day) {
		return nil
	}
	at := func(minutes int) time.Time { return day.Add(time.Duration(minutes) * time.Minute) }
	return []span{{at(morningOpen), at(morningClose)}, {at(afternoonOpen), at(afternoonClose)}}
}

// SessionDeadline is the time at which d of trading-session time has passed
// since t. Time outside the sessions (the lunch break, the evening, nights,
// weekends and holidays) does not count, so a wait that has to span the lunch
// break or the close ends in a session instead of inside the closure. For a
// t and d within one session it is t + d. d <= 0 gives t.
func SessionDeadline(cal Calendar, t time.Time, d time.Duration) time.Time {
	if d <= 0 {
		return t
	}
	cur, remaining := t.In(jst), d
	for range maxLookback {
		for _, s := range sessionsOn(cal, cur) {
			if !s.to.After(cur) {
				continue
			}
			start := s.from
			if cur.After(start) {
				start = cur
			}
			avail := s.to.Sub(start)
			if remaining <= avail {
				return start.Add(remaining)
			}
			remaining -= avail
		}
		cur = time.Date(cur.Year(), cur.Month(), cur.Day()+1, 0, 0, 0, 0, jst)
	}
	return t.Add(d)
}

// SessionSince is the time that is d of trading-session time before t: the
// mirror image of SessionDeadline (SessionDeadline(SessionSince(t, d), d) == t
// when t is inside a session). d <= 0 gives t.
func SessionSince(cal Calendar, t time.Time, d time.Duration) time.Time {
	if d <= 0 {
		return t
	}
	cur, remaining := t.In(jst), d
	for range maxLookback {
		spans := sessionsOn(cal, cur)
		for i := len(spans) - 1; i >= 0; i-- {
			s := spans[i]
			if !s.from.Before(cur) {
				continue
			}
			end := s.to
			if cur.Before(end) {
				end = cur
			}
			avail := end.Sub(s.from)
			if remaining <= avail {
				return end.Add(-remaining)
			}
			remaining -= avail
		}
		// The last instant of the previous JST day.
		cur = time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, jst).Add(-time.Nanosecond)
	}
	return t.Add(-d)
}

// inSession reports whether t is inside a trading session of cal (the same
// 9:00–11:30 / 12:30–15:30 JST the News Ingest session gate uses).
func inSession(cal Calendar, t time.Time) bool {
	return outsideSession(cal, t) == ""
}

// outsideSession says why a fetch cannot start at t (after the lunch break's
// start or the close): "lunch_break" inside 11:30–12:30 of a trading day,
// "after_close" at or after 15:30 (or any other time without a session:
// before the open, weekends, holidays). It is "" inside a session.
func outsideSession(cal Calendar, t time.Time) UnknownDetail {
	t = t.In(jst)
	for _, s := range sessionsOn(cal, t) {
		if !t.Before(s.from) && t.Before(s.to) {
			return ""
		}
	}
	m := t.Hour()*60 + t.Minute()
	if cal.IsTradingDay(t) && m >= morningClose && m < afternoonOpen {
		return DetailLunchBreak
	}
	return DetailAfterClose
}

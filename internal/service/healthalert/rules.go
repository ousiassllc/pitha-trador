// Package healthalert raises the in-app warning for a market-data outage
// during the trading hours (issue #802): kabuステーション not logged in, or
// no market-data job finishing for a while. The Detector persists the active
// alerts (domain.HealthAlert) so Home's 要対応 list and the Paper results MCP,
// a separate read-only process, show the same state; the rules below are
// shared with the MCP, which derives the idle alert itself while the app is
// down. No external notification channel is involved.
package healthalert

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const (
	// IdleThreshold is how long a trading session may pass without a finished
	// market-data job before market_data_idle is raised.
	IdleThreshold = 5 * time.Minute
	// NotLoggedInThreshold is how long a not-logged-in (or, issue #863,
	// connection-refused) streak must last, after the 08:45 check, before
	// kabu_not_logged_in (kabu_unreachable) is raised.
	NotLoggedInThreshold = 5 * time.Minute
	// CheckInterval is how often the Detector evaluates the rules.
	CheckInterval = 30 * time.Second
	// HeartbeatStale is how old the Detector's last check may be before the
	// app counts as not running (four missed checks).
	HeartbeatStale = 2 * time.Minute
)

// kabuCheckMinute is when the morning login check applies, in minutes after
// JST midnight (08:45, a quarter hour before the open).
const kabuCheckMinute = 8*60 + 45

// KabuWindow reports whether now is inside the kabu login watch window (a
// trading day, 08:45 JST up to the 15:30 close, the lunch break included) and
// returns its start, 08:45 of that day.
func KabuWindow(now time.Time) (start time.Time, ok bool) {
	if !marketcalendar.TSE.IsTradingDay(now) {
		return time.Time{}, false
	}
	jst := now.In(marketcalendar.JST)
	start = time.Date(jst.Year(), jst.Month(), jst.Day(), kabuCheckMinute/60, kabuCheckMinute%60, 0, 0, marketcalendar.JST)
	closeAt, _ := marketcalendar.TSE.CloseAt(now)
	if now.Before(start) || !now.Before(closeAt) {
		return time.Time{}, false
	}
	return start, true
}

// KabuNotLoggedInSince decides kabu_not_logged_in for a not-logged-in streak
// that began at streakStart (the zero time when kabu is logged in or fails for
// another reason). It is raised inside KabuWindow when the streak already
// existed at 08:45, or has lasted NotLoggedInThreshold; since is when the
// condition began (the later of the streak start and 08:45).
func KabuNotLoggedInSince(now, streakStart time.Time) (since time.Time, ok bool) {
	return kabuStreakSince(now, streakStart)
}

// KabuUnreachableSince decides kabu_unreachable (issue #863) for a refused-
// connection streak that began at streakStart, by the same rule as
// KabuNotLoggedInSince.
func KabuUnreachableSince(now, streakStart time.Time) (since time.Time, ok bool) {
	return kabuStreakSince(now, streakStart)
}

// kabuStreakSince is the rule both kabu alerts share: inside KabuWindow, a
// streak that already existed at 08:45 raises at once (since 08:45), one that
// began later after NotLoggedInThreshold (since its start).
func kabuStreakSince(now, streakStart time.Time) (since time.Time, ok bool) {
	open, inWindow := KabuWindow(now)
	if !inWindow || streakStart.IsZero() {
		return time.Time{}, false
	}
	if !streakStart.After(open) {
		return open, true
	}
	if now.Sub(streakStart) >= NotLoggedInThreshold {
		return streakStart, true
	}
	return time.Time{}, false
}

// IdleSince decides market_data_idle: inside a trading session, with no
// market-data activity since lastActivity (the zero time when there is none).
// The idle time is counted from the later of lastActivity and the session
// start, so the first minutes of a session and the end of the lunch break are
// not alarms. since is when the idle time began.
func IdleSince(now, lastActivity time.Time) (since time.Time, ok bool) {
	start, inSession := marketcalendar.TSE.SessionStart(now)
	if !inSession {
		return time.Time{}, false
	}
	since = Later(start, lastActivity)
	if now.Sub(since) < IdleThreshold {
		return time.Time{}, false
	}
	return since, true
}

// InWindow reports whether the alert with code may be shown at now: an alert
// is never shown outside the hours it watches (non-trading days, the
// hours before 08:45, after the close, and for market_data_idle the lunch
// break), whatever a stored row says. An unknown code is not shown.
func InWindow(code string, now time.Time) bool {
	switch code {
	case domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertKabuUnreachable:
		_, ok := KabuWindow(now)
		return ok
	case domain.HealthAlertMarketDataIdle:
		return marketcalendar.TSE.IsOpen(now)
	}
	return false
}

// Later returns the later of a and b (time.Time is not ordered for the max
// builtin).
func Later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

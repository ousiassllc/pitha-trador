package risk

import "time"

// MarketCalendar is the trading-session knowledge Engine needs for the
// FR-RISK-6 / FR-RISK-2 gating (non-functional.md §3): whether t is
// inside a 東証立会時間 session, and when t's trading day opens.
// internal/service/marketcalendar.Calendar implements it;
// internal/bootstrap wires it via Config.Calendar.
type MarketCalendar interface {
	IsOpen(t time.Time) bool
	// OpenAt returns the 前場 opening time of t's date, or false when
	// that date is not a trading day.
	OpenAt(t time.Time) (time.Time, bool)
}

// inSession reports whether the session-gated detectors may run at now.
// With no Calendar configured every time counts as in session, so
// detectors stay ungated.
func (e *Engine) inSession(now time.Time) bool {
	return e.calendar == nil || e.calendar.IsOpen(now)
}

// sessionHeartbeat clamps last, the operator's last UI heartbeat, to the
// current trading day's open: FR-RISK-6 counts silence "立会時間中" only,
// so the overnight/weekend gap before the session starts must not make
// the heartbeat stale the moment the market opens. Without a Calendar,
// or on a non-trading day, last is returned unchanged.
func (e *Engine) sessionHeartbeat(last, now time.Time) time.Time {
	if e.calendar == nil {
		return last
	}
	if open, ok := e.calendar.OpenAt(now); ok && last.Before(open) {
		return open
	}
	return last
}

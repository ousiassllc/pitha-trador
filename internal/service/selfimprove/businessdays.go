package selfimprove

import "time"

// businessDaysBefore/businessDaysAfter approximate "N営業日" (FR-
// SELFIMPROVE-4's lookback, FR-SELFIMPROVE-6's tracking window) by
// skipping Saturday/Sunday only - JP market holidays are not modeled,
// matching the rest of this codebase's absence of a holiday calendar
// (internal/service/scheduler's own fullScanInterval cron trigger has
// the same gap). This is a deliberate MVP simplification, not a
// FR-SELFIMPROVE-4/6 correctness requirement: a holiday occasionally
// shifts the lookback/tracking window's exact bar count by one, not its
// overall order-of-magnitude 20/5-day intent.
func businessDaysBefore(t time.Time, days int) time.Time {
	d := t
	for remaining := days; remaining > 0; {
		d = d.AddDate(0, 0, -1)
		if isBusinessDay(d) {
			remaining--
		}
	}
	return d
}

func businessDaysAfter(t time.Time, days int) time.Time {
	d := t
	for remaining := days; remaining > 0; {
		d = d.AddDate(0, 0, 1)
		if isBusinessDay(d) {
			remaining--
		}
	}
	return d
}

func isBusinessDay(t time.Time) bool {
	switch t.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	default:
		return true
	}
}

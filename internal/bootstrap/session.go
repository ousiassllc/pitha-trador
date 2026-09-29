package bootstrap

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// marketcalendarOpen reports whether t is inside a 東証立会時間 session:
// the one predicate the Scheduler, candidate refresh and held-position
// monitor share (the Risk Engine and Execution take marketcalendar.TSE).
func marketcalendarOpen(t time.Time) bool { return marketcalendar.TSE.IsOpen(t) }

// inSession is marketcalendarOpen through s.sessionOpen, which tests
// override; a Services literal without one is always in session.
func (s *Services) inSession(t time.Time) bool {
	return s.sessionOpen == nil || s.sessionOpen(t)
}

// withTradingCalendar gives Execution the TSE calendar: Enter's new-entry
// session gate and OnSnapshot's 引け前強制決済 close time.
func withTradingCalendar(cfg execution.Config) execution.Config {
	cfg.Calendar = marketcalendar.TSE
	return cfg
}

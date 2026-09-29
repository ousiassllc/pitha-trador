package execution

import (
	"errors"
	"time"
)

// ErrOutsideTradingSession is returned by Enter when Config.Calendar says
// the entry time is outside 東証立会時間 (non-functional.md §3).
var ErrOutsideTradingSession = errors.New("execution: outside trading session")

// MarketCalendar is what Engine needs of internal/service/marketcalendar.
type MarketCalendar interface {
	IsOpen(t time.Time) bool
	// CloseAt is the 大引け time of t's trading day; false on a non-trading day.
	CloseAt(t time.Time) (time.Time, bool)
}

// marketCloseAt is MarketContext.MarketCloseAt for an update at now: nil
// (no 引け前強制決済) without a Calendar or on a non-trading day.
func (e *Engine) marketCloseAt(now time.Time) *time.Time {
	if e.cfg.Calendar == nil {
		return nil
	}
	if closeAt, ok := e.cfg.Calendar.CloseAt(now); ok {
		return &closeAt
	}
	return nil
}

// sessionOpen: new entries are allowed at now (always without a Calendar).
func (e *Engine) sessionOpen(now time.Time) bool {
	return e.cfg.Calendar == nil || e.cfg.Calendar.IsOpen(now)
}

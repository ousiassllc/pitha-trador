package execution

import (
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// ErrOutsideTradingSession is returned by Enter when Config.Calendar says
// the entry time is outside 東証立会時間 (non-functional.md §3) or inside the 引け前強制決済 window,
// and by Close's fills during the 昼休み or after the 大引け (nothing
// fills while the market is closed).
var ErrOutsideTradingSession = errors.New("execution: outside trading session")

// MarketCalendar is what Engine needs of internal/service/marketcalendar.
type MarketCalendar interface {
	IsOpen(t time.Time) bool
	// PhaseAt is how an order at t executes: ザラ場, 寄り/引けの板寄せ, or not
	// at all (marketcalendar.PhaseClosed, e.g. the 昼休み).
	PhaseAt(t time.Time) marketcalendar.Phase
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

// sessionOpen: new entries are allowed at now (always without a Calendar):
// in session and before the force-flat time (else it would be flatted at once).
func (e *Engine) sessionOpen(now time.Time) bool {
	if e.cfg.Calendar == nil {
		return true
	}
	closeAt := e.marketCloseAt(now)
	return e.cfg.Calendar.IsOpen(now) &&
		(closeAt == nil || now.Before(closeAt.Add(-time.Duration(e.cfg.ForceFlatBeforeMarketCloseMinutes)*time.Minute)))
}

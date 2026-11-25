package updater

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// InMarketHours reports whether t is within 東証の立会時間帯 of a trading day:
// from the 前場 open (9:00 JST) up to, not including, the 大引け (15:30 JST),
// the lunch break included (issue #1222: 取引日の JST 9:00〜15:30). It only
// reads marketcalendar.TSE's trading-day, open and close times, so holidays and
// the year-end closure follow the one calendar the Scheduler and Execution use.
//
// Only the Paper results MCP's update_apply waits on it (mcpbridge); SafeGate,
// the periodic check and the Settings panel's installs do not.
func InMarketHours(t time.Time) bool {
	open, ok := marketcalendar.TSE.OpenAt(t)
	if !ok {
		return false
	}
	closeAt, _ := marketcalendar.TSE.CloseAt(t)
	return !t.Before(open) && t.Before(closeAt)
}

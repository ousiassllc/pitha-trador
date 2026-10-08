package tachibana

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// JST is the zone every adapter time is expressed in.
var JST = time.FixedZone("JST", 9*60*60)

// Times of day (JST, offsets from midnight) the adapter's behavior hangs on
// (integrations.md §5.3, non-functional.md §2.3/§3).
const (
	// CloseAt is the daily 閉局: every virtual URL stops working then.
	CloseAt = 3*time.Hour + 30*time.Minute
	// OpenAt is when logging in works again (03:30〜05:30 is closed).
	OpenAt = 5*time.Hour + 30*time.Minute
	// LoginDeadline is when a morning login still missing is reported (Slack).
	LoginDeadline = 8*time.Hour + 30*time.Minute

	// daytimeStart/daytimeEnd bound the 日中 window (8:00〜15:30) in which the
	// broker asks for restraint: low-priority bulk requests never leave the
	// queue then (PriorityHistory).
	daytimeStart = 8 * time.Hour
	daytimeEnd   = 15*time.Hour + 30*time.Minute
)

// TimeOfDay is t's offset from midnight JST.
func TimeOfDay(t time.Time) time.Duration {
	t = t.In(JST)
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
}

// AtClock is the instant at offset d from midnight on t's JST date.
func AtClock(t time.Time, d time.Duration) time.Time {
	y, m, day := t.In(JST).Date()
	return time.Date(y, m, day, 0, 0, 0, 0, JST).Add(d)
}

// NextClock is the first instant at or after t whose JST time of day is d.
func NextClock(t time.Time, d time.Duration) time.Time {
	c := AtClock(t, d)
	if c.Before(t) {
		c = c.Add(24 * time.Hour)
	}
	return c
}

// NextClose is the first 03:30 JST strictly after t: how long a login at t
// stays valid.
func NextClose(t time.Time) time.Time {
	c := AtClock(t, CloseAt)
	if !c.After(t) {
		c = c.Add(24 * time.Hour)
	}
	return c
}

// InClosedWindow reports whether t is in the 03:30〜05:30 JST window where
// the broker is closed and refuses logins (p_errno=-62).
func InClosedWindow(t time.Time) bool {
	d := TimeOfDay(t)
	return d >= CloseAt && d < OpenAt
}

// InDaytime reports whether t is in 8:00〜15:30 JST.
func InDaytime(t time.Time) bool {
	d := TimeOfDay(t)
	return d >= daytimeStart && d < daytimeEnd
}

// DayKey is t's JST date as YYYYMMDD, the format of the broker's notice dates.
func DayKey(t time.Time) string { return t.In(JST).Format("20060102") }

// ParseClockOfDay parses "H:MM"/"HH:MM". An empty or malformed value is the
// default re-login time (config.DefaultTachibanaReauthTime); the Settings
// screen validates what is stored, this only keeps a corrupt row harmless.
func ParseClockOfDay(s string) time.Duration {
	if h, m, ok := splitClock(s); ok {
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	}
	h, m, _ := splitClock(config.DefaultTachibanaReauthTime)
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
}

func splitClock(s string) (h, m int, ok bool) {
	hh, mm, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// FormatClock renders an offset from midnight as HH:MM.
func FormatClock(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

package organisms

import (
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// Formatting helpers of ScanPanel (scan_panel.templ).

var weekdayJA = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// formatNextOpen renders t as JST, e.g. `2026-10-05(月) 09:00 JST`. The
// zone is fixed so the output does not depend on the host's local zone.
func formatNextOpen(t time.Time) string {
	t = t.In(atoms.JST)
	return t.Format("2006-01-02") + "(" + weekdayJA[t.Weekday()] + ") " + t.Format("15:04") + " JST"
}

func formatCycleDuration(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
}

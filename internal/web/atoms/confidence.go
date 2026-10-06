package atoms

import (
	"math"
	"strconv"
)

// FormatConfidence renders a 0..1 Jev confidence as a whole percent
// ("13%"), or "—" when it is nil (not evaluated yet). It is the single
// rounding rule for every SSR confidence cell (Scanner fallback table and
// Symbol Detail's SignalBadgeGroup): halves round away from zero like
// Math.round in scanner-view.ts's formatConfidence, so the same value
// reads the same on every screen (fmt's %.0f would round half to even:
// 0.125 -> "12%", issue #677).
func FormatConfidence(v *float64) string {
	if v == nil {
		return "—"
	}
	pct := math.Round(*v * 100)
	if pct == 0 {
		pct = 0 // drops the sign of -0, like `${-0}` in JS
	}
	return strconv.FormatFloat(pct, 'f', 0, 64) + "%"
}

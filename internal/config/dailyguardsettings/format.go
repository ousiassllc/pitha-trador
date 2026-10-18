package dailyguardsettings

import (
	"strconv"
	"strings"
)

// FormatNumber renders v without a trailing ".0" (1, 0.5, 33.33).
func FormatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// FormatYen renders v with thousands separators (5,000; 1,234,567.5). It is
// not shared with organisms.yen, which rounds to whole yen and adds a ¥ sign.
func FormatYen(v float64) string {
	text := FormatNumber(v)
	intPart, frac, hasFrac := strings.Cut(text, ".")
	negative := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	out := b.String()
	if negative {
		out = "-" + out
	}
	if hasFrac {
		out += "." + frac
	}
	return out
}

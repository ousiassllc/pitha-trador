package homepanels

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// FormatYen renders a yen amount with thousands separators, rounded to the
// yen: 12345.4 -> "12,345円".
func FormatYen(v float64) string { return groupDigits(int64(math.Round(v))) + "円" }

// FormatSignedYen is FormatYen with an explicit sign: "+1,234円" / "-1,234円",
// and "0円" for zero.
func FormatSignedYen(v float64) string {
	n := int64(math.Round(v))
	if n > 0 {
		return "+" + groupDigits(n) + "円"
	}
	return groupDigits(n) + "円"
}

// FormatPrice renders a share price with thousands separators and no more
// decimals than it has (to two): 4242.5 -> "4,242.5".
func FormatPrice(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	whole, frac, _ := strings.Cut(s, ".")
	n, _ := strconv.ParseInt(whole, 10, 64)
	out := groupDigits(n)
	if frac != "" {
		out += "." + frac
	}
	if v < 0 {
		out = "-" + out
	}
	return out
}

// groupDigits writes n with a comma every three digits.
func groupDigits(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

// PnLClass colours a profit green and a loss red; zero is plain.
func PnLClass(v float64) string {
	switch n := math.Round(v); {
	case n > 0:
		return "text-emerald-700"
	case n < 0:
		return "text-red-700"
	}
	return "text-slate-700"
}

// FormatHeld renders how long a position has been held: "45分", "2時間5分",
// "1日3時間".
func FormatHeld(d time.Duration) string {
	minutes := int(d / time.Minute)
	switch {
	case minutes < 60:
		return strconv.Itoa(minutes) + "分"
	case minutes < 24*60:
		return strconv.Itoa(minutes/60) + "時間" + strconv.Itoa(minutes%60) + "分"
	}
	return strconv.Itoa(minutes/(24*60)) + "日" + strconv.Itoa(minutes/60%24) + "時間"
}

// FormatTime renders t as JST month-day and clock time: "10-22 09:15".
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(atoms.JST).Format("01-02 15:04")
}

// SideLabel names a position side the way a trader reads it.
func SideLabel(side string) string {
	switch side {
	case "LONG":
		return "買い"
	case "SHORT":
		return "売り"
	}
	return side
}

// SymbolURL is the Symbol Detail page of symbol.
func SymbolURL(symbol string) templ.SafeURL {
	return templ.URL("/symbols/" + url.PathEscape(symbol))
}

// ProposalURL is the detail page of an order proposal.
func ProposalURL(id int64) templ.SafeURL {
	return templ.URL("/order-proposals/" + strconv.FormatInt(id, 10))
}

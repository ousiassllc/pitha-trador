package mcpsettings

import (
	"math"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Figure and time formatters of the experiments view, split out of
// experiments_view.go to keep it within the linterly per-file limit.

// optYen2 is a yen figure rounded to 0.01, "—" when there is none.
func optYen2(v *float64) string {
	if v == nil {
		return "—"
	}
	return yen(math.Round(*v*100) / 100)
}

func optSignedYen2(v *float64) string {
	if v == nil {
		return "—"
	}
	return signedYen(math.Round(*v*100) / 100)
}

func jst(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.In(query.JST).Format("2006-01-02 15:04")
}

func yen(v float64) string { return dailyguardsettings.FormatYen(v) + " 円" }

func signedYen(v float64) string {
	if v > 0 {
		return "+" + yen(v)
	}
	return yen(v)
}

func signedInt(v int) string {
	if v > 0 {
		return "+" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func optYen(v *float64) string {
	if v == nil {
		return "—"
	}
	return yen(*v)
}

func percent(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v*100, 'f', 1, 64) + " %"
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

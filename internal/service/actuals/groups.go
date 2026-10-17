package actuals

import (
	"cmp"
	"fmt"
	"slices"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// groupBy splits trades (ordered by ClosedAt, which each slice keeps) by key
// and summarizes each slice, ordered by key with "" (no value) last.
func groupBy(trades []repo.Trade, capital float64, c Criteria, key func(repo.Trade) string) []Group {
	slice := map[string][]repo.Trade{}
	for _, t := range trades {
		k := key(t)
		slice[k] = append(slice[k], t)
	}
	keys := make([]string, 0, len(slice))
	for k := range slice {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if (a == "") != (b == "") {
			return cmp.Compare(b, a) // "" sorts after any other key
		}
		return cmp.Compare(a, b)
	})
	out := make([]Group, 0, len(keys))
	for _, k := range keys {
		out = append(out, Group{Key: k, Stats: summarize(slice[k], capital, c)})
	}
	return out
}

func dayKey(t repo.Trade) string { return t.ClosedAt.In(jst).Format("2006-01-02") }

// weekKey is the ISO 8601 week of the JST close day, e.g. "2026-W42".
func weekKey(t repo.Trade) string {
	y, w := t.ClosedAt.In(jst).ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// groupings fills every slice of e from trades ordered by ClosedAt.
func groupings(e *Evaluation, trades []repo.Trade, capital float64, c Criteria) {
	e.ByDay = groupBy(trades, capital, c, dayKey)
	e.ByWeek = groupBy(trades, capital, c, weekKey)
	e.ByDirection = groupBy(trades, capital, c, func(t repo.Trade) string { return t.Side })
	e.ByExitReason = groupBy(trades, capital, c, func(t repo.Trade) string { return t.ExitReason })
	e.ByMetaModel = groupBy(trades, capital, c, func(t repo.Trade) string { return t.MetaModelVersion })
	e.ByABArm = groupBy(trades, capital, c, func(t repo.Trade) string { return t.ABArm })
}

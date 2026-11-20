package slicediag

import (
	"cmp"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// Tercile bucket names, lowest value first.
var tercileNames = []string{"low", "mid", "high"}

// terciles cuts the cohort's rows with a value into thirds by rank: sorted by value,
// ties broken by decision id (so the cut is deterministic, and equal values can fall
// on both sides of a boundary), the i-th of k rows is in tercile i*3/k. Rows without
// the value are unknown. The thirds are computed over the cohort's evaluated rows, so
// they move with the data: they are bands of this cohort, not fixed levels.
func terciles(value func(evaldiag.Attrs) *float64) func([]evaldiag.Row) assignment {
	return func(rows []evaldiag.Row) assignment {
		a := assignment{labels: make([]string, len(rows)), order: append(slices.Clone(tercileNames), bucketUnknown)}
		var ranked []int
		for i, r := range rows {
			if value(r.Attrs) == nil {
				a.labels[i] = bucketUnknown
				continue
			}
			ranked = append(ranked, i)
		}
		slices.SortFunc(ranked, func(x, y int) int {
			return cmp.Or(cmp.Compare(*value(rows[x].Attrs), *value(rows[y].Attrs)), cmp.Compare(rows[x].DecisionID, rows[y].DecisionID))
		})
		k := len(ranked)
		firstOf := map[string]float64{}
		for rank, i := range ranked {
			name := tercileNames[rank*3/k]
			a.labels[i] = name
			if _, ok := firstOf[name]; !ok {
				firstOf[name] = *value(rows[i].Attrs)
			}
		}
		if k >= 3 {
			a.edges = []float64{firstOf["mid"], firstOf["high"]}
		}
		return a
	}
}

// sectors is the instrument sector: the MaxSectorBuckets sectors with the most rows
// (ties by name) keep their own bucket, the rest are "other", a row without a sector is
// unknown. Buckets are listed by name (not by size or result); "other" and unknown are
// listed only when a row falls in them.
func sectors(rows []evaldiag.Row) assignment {
	count := map[string]int{}
	for _, r := range rows {
		if s := r.Attrs.Sector; s != "" && s != bucketOther && s != bucketUnknown { // a sector called like a synthetic bucket joins it
			count[s]++
		}
	}
	names := make([]string, 0, len(count))
	for name := range count {
		names = append(names, name)
	}
	slices.SortFunc(names, func(x, y string) int { return cmp.Or(-cmp.Compare(count[x], count[y]), cmp.Compare(x, y)) })
	kept := map[string]bool{}
	for _, name := range names[:min(len(names), MaxSectorBuckets)] {
		kept[name] = true
	}
	a := assignment{labels: make([]string, len(rows))}
	hasOther, hasUnknown := false, false
	for i, r := range rows {
		switch {
		case r.Attrs.Sector == "" || r.Attrs.Sector == bucketUnknown:
			a.labels[i], hasUnknown = bucketUnknown, true
		case kept[r.Attrs.Sector]:
			a.labels[i] = r.Attrs.Sector
		default:
			a.labels[i], hasOther = bucketOther, true
		}
	}
	for name := range kept {
		a.order = append(a.order, name)
	}
	slices.Sort(a.order)
	if hasOther {
		a.order = append(a.order, bucketOther)
	}
	if hasUnknown {
		a.order = append(a.order, bucketUnknown)
	}
	return a
}

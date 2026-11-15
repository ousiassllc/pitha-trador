package slicediag

import (
	"context"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// bucketOf summarizes the LLM arm's net bps of the rows (the same day-cluster
// bootstrap, constants and p-value as evaldiag.Summarize) and flags a small sample.
func bucketOf(name string, rows []evaldiag.Row) Bucket {
	points := make([]evaldiag.Point, len(rows))
	for i, r := range rows {
		points[i] = evaldiag.Point{Day: r.Day, Value: r.LLM}
	}
	s := evaldiag.Summarize(points, false)
	b := Bucket{
		Bucket: name, N: s.N, Days: s.Days, MeanNetBps: s.MeanNetBps, CI: s.CI, WinRate: s.WinRate,
		DetectableMinimumEdgeBps: s.DetectableMinimumEdgeBps, PValue: s.P, LowSampleReasons: []string{},
	}
	if s.N < MinBucketRows {
		b.LowSampleReasons = append(b.LowSampleReasons, ReasonRowsBelowMin)
	}
	if s.Days < MinBucketDays {
		b.LowSampleReasons = append(b.LowSampleReasons, ReasonDaysBelowMin)
	}
	b.LowSample = len(b.LowSampleReasons) > 0
	return b
}

// cutCohort cuts one cohort's rows by every dimension of every family and applies
// Holm inside each family, over all the family's buckets that have a p-value (a
// low-sample bucket stays in; so does a dimension that was not asked for, so the
// correction never depends on which dimensions are shown). want selects the dimensions
// that are returned. ctx is checked between buckets: the bootstrap is the cost.
func cutCohort(ctx context.Context, rows []evaldiag.Row, want []string) (Cohort, error) {
	c := Cohort{Evaluated: len(rows), All: bucketOf("all", rows), Families: []Family{}}
	for _, family := range []string{FamilyPreregistered, FamilyExploratory} {
		f := Family{Family: family, Dimensions: []Dimension{}}
		var tested []*Bucket // every bucket with a p-value, in output order
		for _, d := range dimensions {
			if d.family != family {
				continue
			}
			a := d.assign(rows)
			dim := Dimension{Dimension: d.name, Rows: len(rows), Edges: a.edges, Buckets: make([]Bucket, 0, len(a.order))}
			byLabel := make(map[string][]evaldiag.Row, len(a.order))
			for i, r := range rows {
				byLabel[a.labels[i]] = append(byLabel[a.labels[i]], r)
			}
			for _, name := range a.order {
				if err := ctx.Err(); err != nil {
					return Cohort{}, err
				}
				dim.Buckets = append(dim.Buckets, bucketOf(name, byLabel[name]))
			}
			f.Buckets += len(dim.Buckets)
			f.Dimensions = append(f.Dimensions, dim)
		}
		for i := range f.Dimensions {
			for j := range f.Dimensions[i].Buckets {
				if b := &f.Dimensions[i].Buckets[j]; b.PValue != nil {
					tested = append(tested, b)
				}
			}
		}
		raw := make([]float64, len(tested))
		for i, b := range tested {
			raw[i] = *b.PValue
		}
		for i, adj := range stats.Holm(raw) {
			tested[i].HolmAdjustedP = &adj
		}
		f.FamilySize = len(tested)
		f.Dimensions = slices.DeleteFunc(f.Dimensions, func(d Dimension) bool { return !slices.Contains(want, d.Dimension) })
		if len(f.Dimensions) > 0 {
			c.Families = append(c.Families, f)
		}
	}
	return c, nil
}

package actuals

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// jevObs accumulates the graded observations of one Jev slice.
type jevObs struct {
	hit, brier []float64
	days       []int64
}

func (o *jevObs) add(s repo.JevSample) {
	outcome := 0.0
	if s.Correct {
		outcome = 1
	}
	o.hit = append(o.hit, outcome)
	o.brier = append(o.brier, (s.Confidence-outcome)*(s.Confidence-outcome))
	o.days = append(o.days, jstDay(s.DecidedAt))
}

func (o *jevObs) metrics(c Criteria) (hit, brier Metric) {
	return proportionMetric(o.hit, o.days, c), meanMetric(o.brier, o.days, c)
}

// jevSliceKeys are the dimensions a label version's samples are sliced by.
var jevSliceKeys = []struct {
	key func(repo.JevSample) string
	set func(*JevGroup, []JevSlice)
}{
	{func(s repo.JevSample) string { return s.DecidedAt.In(jst).Format("2006-01-02") }, func(g *JevGroup, v []JevSlice) { g.ByDay = v }},
	{func(s repo.JevSample) string {
		y, w := s.DecidedAt.In(jst).ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	}, func(g *JevGroup, v []JevSlice) { g.ByWeek = v }},
	{func(s repo.JevSample) string { return s.Direction }, func(g *JevGroup, v []JevSlice) { g.ByDirection = v }},
	{func(s repo.JevSample) string { return strconv.Itoa(s.HorizonMinutes) }, func(g *JevGroup, v []JevSlice) { g.ByHorizon = v }},
}

// jevGroups computes Jev's direction hit rate and Brier score per
// calibration label version over samples, overall and sliced by decision day
// (JST), ISO week, direction and grading horizon. A decision graded at
// several horizons contributes one observation per horizon; they fall on the
// same day, so the day clustering of the intervals keeps them from
// overstating the certainty. Versions are ordered ascending; the slices of a
// version are ordered by key (numerically for the horizon).
func jevGroups(samples []repo.JevSample, c Criteria) []JevGroup {
	byVersion := map[int][]repo.JevSample{}
	for _, s := range samples {
		byVersion[s.LabelVersion] = append(byVersion[s.LabelVersion], s)
	}
	versions := make([]int, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	out := make([]JevGroup, 0, len(versions))
	for _, v := range versions {
		g := JevGroup{LabelVersion: v}
		var all jevObs
		for _, s := range byVersion[v] {
			all.add(s)
		}
		g.HitRate, g.Brier = all.metrics(c)
		for _, d := range jevSliceKeys {
			d.set(&g, jevSlices(byVersion[v], c, d.key))
		}
		out = append(out, g)
	}
	return out
}

// jevSlices groups samples by key; keys are ordered by length then text,
// which is numeric for the horizon's non-negative integers and chronological
// for the fixed-width day and week keys ("LONG" precedes "SHORT" either way).
func jevSlices(samples []repo.JevSample, c Criteria, key func(repo.JevSample) string) []JevSlice {
	slice := map[string]*jevObs{}
	for _, s := range samples {
		k := key(s)
		if slice[k] == nil {
			slice[k] = &jevObs{}
		}
		slice[k].add(s)
	}
	keys := make([]string, 0, len(slice))
	for k := range slice {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(a), len(b)), cmp.Compare(a, b))
	})
	out := make([]JevSlice, 0, len(keys))
	for _, k := range keys {
		hit, brier := slice[k].metrics(c)
		out = append(out, JevSlice{Key: k, HitRate: hit, Brier: brier})
	}
	return out
}

package actuals

import (
	"slices"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// jevGroups computes Jev's direction hit rate and Brier score per
// calibration label version over samples. A decision graded at several
// horizons contributes one observation per horizon; they fall on the same
// day, so the day clustering of the intervals keeps them from overstating the
// certainty. Versions are ordered ascending.
func jevGroups(samples []repo.JevSample, c Criteria) []JevGroup {
	type obs struct {
		hit, brier []float64
		days       []int64
	}
	byVersion := map[int]*obs{}
	for _, s := range samples {
		o := byVersion[s.LabelVersion]
		if o == nil {
			o = &obs{}
			byVersion[s.LabelVersion] = o
		}
		outcome := 0.0
		if s.Correct {
			outcome = 1
		}
		o.hit = append(o.hit, outcome)
		o.brier = append(o.brier, (s.Confidence-outcome)*(s.Confidence-outcome))
		o.days = append(o.days, jstDay(s.DecidedAt))
	}
	versions := make([]int, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	out := make([]JevGroup, 0, len(versions))
	for _, v := range versions {
		o := byVersion[v]
		out = append(out, JevGroup{
			LabelVersion: v,
			HitRate:      proportionMetric(o.hit, o.days, c),
			Brier:        meanMetric(o.brier, o.days, c),
		})
	}
	return out
}

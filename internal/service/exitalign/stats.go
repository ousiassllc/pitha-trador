package exitalign

import (
	"math"
	"sort"
)

// summarize computes the DistributionBps of values.
func summarize(values []float64) DistributionBps {
	n := len(values)
	if n == 0 {
		return DistributionBps{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)
	var sq float64
	for _, v := range sorted {
		sq += (v - mean) * (v - mean)
	}
	return DistributionBps{
		Count: n, Mean: mean, StdDev: math.Sqrt(sq / float64(n)),
		Min: sorted[0], P5: percentile(sorted, 0.05), P25: percentile(sorted, 0.25), Median: percentile(sorted, 0.5),
		P75: percentile(sorted, 0.75), P95: percentile(sorted, 0.95), Max: sorted[n-1],
	}
}

// percentile is the linearly interpolated q-quantile (0..1) of sorted.
func percentile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// reasonCounts returns counts as a slice ordered by count (descending) then
// reason.
func reasonCounts(counts map[string]int) []ReasonCount {
	out := make([]ReasonCount, 0, len(counts))
	for reason, n := range counts {
		out = append(out, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

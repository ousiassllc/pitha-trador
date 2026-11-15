package researchquotediag

import (
	"math"
	"slices"
)

// gapAcc collects the paired gaps of one group.
type gapAcc struct {
	abs    []float64
	signed float64
}

func (g *gapAcc) add(bps float64) {
	g.abs = append(g.abs, math.Abs(bps))
	g.signed += bps
}

func (g *gapAcc) merge(o *gapAcc) {
	g.abs = append(g.abs, o.abs...)
	g.signed += o.signed
}

// gap summarizes the collected gaps (it reorders abs).
func (g *gapAcc) gap() Gap {
	n := len(g.abs)
	if n == 0 {
		return Gap{}
	}
	slices.Sort(g.abs)
	var sum float64
	above := [3]int{}
	for _, a := range g.abs {
		sum += a
		for i, limit := range [3]float64{5, 10, 50} {
			if a > limit {
				above[i]++
			}
		}
	}
	share := func(i int) *float64 { v := float64(above[i]) / float64(n); return &v }
	mean, signed, maxAbs := sum/float64(n), g.signed/float64(n), g.abs[n-1]
	return Gap{
		Pairs: n, MeanAbsBps: &mean, MedianBps: ptr(median(g.abs)), P95AbsBps: ptr(g.abs[(n*95+99)/100-1]),
		MaxAbsBps: &maxAbs, MeanBps: &signed, ShareAbove5: share(0), ShareAbove10: share(1), ShareAbove50: share(2),
	}
}

func ptr(v float64) *float64 { return &v }

// median of sorted values.
func median(sorted []float64) float64 {
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// cmpAcc counts the minutes of one group and collects the gaps.
type cmpAcc struct {
	both, onlyKabu, onlyTachibana int
	gaps                          gapAcc
}

func (c *cmpAcc) merge(o *cmpAcc) {
	c.both += o.both
	c.onlyKabu += o.onlyKabu
	c.onlyTachibana += o.onlyTachibana
	c.gaps.merge(&o.gaps)
}

func (c *cmpAcc) comparison() Comparison {
	return Comparison{MinutesBoth: c.both, OnlyKabu: c.onlyKabu, OnlyTachibana: c.onlyTachibana, Gap: c.gaps.gap()}
}

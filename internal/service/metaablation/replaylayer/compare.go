package replaylayer

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// pnlPath is one cell's per-day P&L on one segment, the input of a comparison.
type pnlPath struct {
	segment int
	capital float64
	days    []dayPnL
}

// pick is the stream's value of a day.
func (d dayPnL) pick(stream string) float64 {
	switch stream {
	case StreamMomentum:
		return d.momentum
	case StreamReversion:
		return d.reversion
	default:
		return d.total
	}
}

// diffSeries aligns two cells' days and returns B - A. The cells share the same
// days by construction; a mismatch is an error, never silently truncated.
func diffSeries(a, b pnlPath, stream, unit string) (DiffSeries, error) {
	if len(a.days) != len(b.days) {
		return DiffSeries{}, fmt.Errorf("replaylayer: segment %d: the cells have different days (%d vs %d)", a.segment, len(a.days), len(b.days))
	}
	scale := 1.0
	if unit == UnitCapitalRatio {
		scale = 1 / a.capital
	}
	s := DiffSeries{Stream: stream, Unit: unit, Days: make([]DailyDiff, len(a.days))}
	var sum float64
	for i := range a.days {
		if a.days[i].day != b.days[i].day {
			return DiffSeries{}, fmt.Errorf("replaylayer: segment %d: day %s is not aligned with %s", a.segment, a.days[i].day, b.days[i].day)
		}
		va, vb := a.days[i].pick(stream)*scale, b.days[i].pick(stream)*scale
		s.Days[i] = DailyDiff{Day: a.days[i].day, A: va, B: vb, Diff: vb - va}
		sum += vb - va
	}
	if len(s.Days) > 0 {
		s.Mean = sum / float64(len(s.Days))
	}
	return s, nil
}

// axisOf names the axes a comparison changes.
func axisOf(c metaablation.Comparison) string {
	m, q := c.From.MOn != c.To.MOn, c.From.QOn != c.To.QOn
	switch {
	case m && q:
		return "both"
	case q:
		return "q"
	default:
		return "m"
	}
}

// compare builds every comparison of metaablation.Comparisons() over the cells'
// paths (paths[cell][segment], cells in metaablation.Cells() order): for each
// segment the aligned daily differences of the total, momentum and reversion
// streams, in yen and as a ratio of the segment's initial capital. Diagonals
// stay Primary=false (reference only). The Q axis is based on the momentum
// stream: reversion candidates are not affected by Q.
func compare(experiment string, paths [][]pnlPath) ([]ComparisonResult, error) {
	cells := metaablation.Cells()
	at := func(c metaablation.Cell) int {
		for i, x := range cells {
			if x == c {
				return i
			}
		}
		return -1
	}
	var out []ComparisonResult
	for _, cmp := range metaablation.Comparisons() {
		axis := axisOf(cmp)
		res := ComparisonResult{
			Experiment: experiment, Name: cmp.Name, From: cmp.From, To: cmp.To, Primary: cmp.Primary,
			Axis: axis, BasisStream: StreamTotal,
		}
		if axis == "q" {
			res.BasisStream = StreamMomentum
		}
		a, b := paths[at(cmp.From)], paths[at(cmp.To)]
		for seg := range a {
			cs := ComparisonSegment{Segment: a[seg].segment}
			for _, stream := range []string{StreamTotal, StreamMomentum, StreamReversion} {
				for _, unit := range []string{UnitJPY, UnitCapitalRatio} {
					s, err := diffSeries(a[seg], b[seg], stream, unit)
					if err != nil {
						return nil, err
					}
					cs.Series = append(cs.Series, s)
				}
			}
			res.Segments = append(res.Segments, cs)
		}
		out = append(out, res)
	}
	return out, nil
}

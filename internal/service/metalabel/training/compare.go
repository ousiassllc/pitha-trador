package training

import (
	"fmt"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// Compare evaluates the candidate against the active model on the very same
// OOS rows (issue #780). Each model's return per decision opportunity is its
// taken signals' net return, a skip earning 0, so models taking different
// signals compare on one scale; the difference candidate - active gets a
// cluster-bootstrap interval over the rows both could score. The result is not
// Comparable when too few rows are shared or the interval is undefined.
func Compare(candidate, active Evaluation, c AcceptanceConfig, seed uint64, activeVersion string) repo.Comparison {
	out := repo.Comparison{ActiveVersion: activeVersion}
	activeByIndex := make(map[int]scoredRow, len(active.scored))
	for _, r := range active.scored {
		activeByIndex[r.index] = r
	}
	var paired, activeRows []scoredRow
	var diffs []float64
	var candSum, activeSum float64
	for _, r := range candidate.scored {
		a, ok := activeByIndex[r.index]
		if !ok {
			continue
		}
		paired = append(paired, r)
		activeRows = append(activeRows, a)
		cand, act := takenReturn(r), takenReturn(a)
		diffs = append(diffs, cand-act)
		candSum += cand
		activeSum += act
	}
	out.PairedSamples = len(paired)
	if len(paired) < c.MinOOSSamples {
		out.Note = fmt.Sprintf("the active model scored %d of the candidate's %d OOS rows (minimum %d)", len(paired), len(candidate.scored), c.MinOOSSamples)
		return out
	}
	n := float64(len(paired))
	out.CandidateBps, out.ActiveBps, out.DiffBps = candSum/n, activeSum/n, (candSum-activeSum)/n

	cl := newClusters(paired)
	sum, count := make([]float64, cl.n), make([]float64, cl.n)
	for i, d := range diffs {
		sum[cl.of[i]] += d
		count[cl.of[i]]++
	}
	out.DiffCI = ratioInterval(cl, sum, count, c.Resamples, c.Alpha, newRNG(seed, 3))
	activeClusters := newClusters(activeRows)
	out.ActiveEVCI = active.evInterval(activeClusters, c, seed)
	if out.DiffCI == nil {
		out.Note = "the difference has no interval (too few trading days)"
		return out
	}
	out.Comparable = true
	return out
}

func takenReturn(r scoredRow) float64 {
	if r.take {
		return r.ret
	}
	return 0
}

// judgeComparison decides whether the candidate may replace the active model.
// A model the active one cannot be compared with is never swapped in. While the
// active model still passes the bar on this OOS (the lower limit of its own EV
// is above the minimum) the candidate must beat it significantly (the lower
// limit of the difference above 0); once it no longer does, the candidate only
// has to be no worse (a difference of at least 0), having passed the bar itself.
func (c AcceptanceConfig) judgeComparison(cmp repo.Comparison) (adopt bool, reason string) {
	switch {
	case !cmp.Comparable || cmp.DiffCI == nil:
		return false, RejectNotComparable
	case cmp.ActiveEVCI != nil && cmp.ActiveEVCI.Low > c.MinEVLowerBps:
		if cmp.DiffCI.Low > 0 {
			return true, ""
		}
		return false, RejectNotBetter
	case cmp.DiffBps >= 0:
		return true, ""
	}
	return false, RejectNotBetter
}

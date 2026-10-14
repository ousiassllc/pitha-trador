package calibration

import "github.com/ousiassllc/pitha-trador/internal/domain"

// barrierAccumulator sums triple-barrier labels for one aggregation group
// (the whole sample set or one confidence band).
type barrierAccumulator struct {
	count, tp, sl, timeout int
	netBpsSum              float64
}

// add counts sample when it is a triple-barrier label (it has a barrier and
// a net return); legacy close-only labels are skipped.
func (a *barrierAccumulator) add(sample domain.LabeledSample) {
	if sample.BarrierHit == "" || sample.NetReturnBps == nil {
		return
	}
	switch sample.BarrierHit {
	case domain.BarrierHitTakeProfit:
		a.tp++
	case domain.BarrierHitStopLoss:
		a.sl++
	case domain.BarrierHitTimeout:
		a.timeout++
	default:
		return
	}
	a.count++
	a.netBpsSum += *sample.NetReturnBps
}

// stats returns the group's BarrierStats: the EV (mean net return, bps) and
// each barrier's share of the labels, all 0 for an empty group.
func (a barrierAccumulator) stats() domain.BarrierStats {
	s := domain.BarrierStats{SampleCount: a.count, TPCount: a.tp, SLCount: a.sl, TimeoutCount: a.timeout}
	if a.count > 0 {
		n := float64(a.count)
		s.TPRate, s.SLRate, s.TimeoutRate = float64(a.tp)/n, float64(a.sl)/n, float64(a.timeout)/n
		s.EVNetBps = a.netBpsSum / n
	}
	return s
}

package calibration_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

func barrierSample(confidence float64, hit string, netBps float64) domain.LabeledSample {
	return domain.LabeledSample{
		Direction: domain.JevDirectionLong, Confidence: confidence, HorizonMinutes: 10,
		WasDirectionCorrect: netBps > 0, BarrierHit: hit, NetReturnBps: &netBps,
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Issue #767: EV (net) is the mean net return and each barrier's rate its
// share of the triple-barrier labels, overall and per confidence band.
func TestMetrics_TripleBarrierEVAndReachRates(t *testing.T) {
	m := calibration.Metrics([]domain.LabeledSample{
		barrierSample(0.55, domain.BarrierHitTakeProfit, 30),
		barrierSample(0.55, domain.BarrierHitStopLoss, -30),
		barrierSample(0.55, domain.BarrierHitStopLoss, -30),
		barrierSample(0.55, domain.BarrierHitTimeout, -10),
		barrierSample(0.65, domain.BarrierHitTakeProfit, 30),
		barrierSample(0.65, domain.BarrierHitTimeout, 20),
	})

	b := m.Barrier
	if b.SampleCount != 6 || b.TPCount != 2 || b.SLCount != 2 || b.TimeoutCount != 2 {
		t.Fatalf("Barrier counts = %+v, want 6 samples split 2/2/2", b)
	}
	if !near(b.TPRate, 1.0/3) || !near(b.SLRate, 1.0/3) || !near(b.TimeoutRate, 1.0/3) {
		t.Errorf("rates tp/sl/timeout = %v/%v/%v, want 1/3 each", b.TPRate, b.SLRate, b.TimeoutRate)
	}
	if want := (30.0 - 30 - 30 - 10 + 30 + 20) / 6; !near(b.EVNetBps, want) {
		t.Errorf("EVNetBps = %v, want %v", b.EVNetBps, want)
	}

	low := m.Buckets[0].Barrier // 0.50-0.60
	if low.SampleCount != 4 || low.TPCount != 1 || low.SLCount != 2 || low.TimeoutCount != 1 {
		t.Fatalf("0.50-0.60 Barrier counts = %+v, want 4 samples split 1/2/1", low)
	}
	if !near(low.SLRate, 0.5) || !near(low.EVNetBps, -10) {
		t.Errorf("0.50-0.60 sl_rate/ev = %v/%v, want 0.5/-10", low.SLRate, low.EVNetBps)
	}
	if mid := m.Buckets[1].Barrier; mid.SampleCount != 2 || !near(mid.EVNetBps, 25) {
		t.Errorf("0.60-0.70 Barrier = %+v, want 2 samples, ev 25", mid)
	}
	if empty := m.Buckets[4].Barrier; empty != (domain.BarrierStats{}) {
		t.Errorf("0.90-1.00 Barrier = %+v, want zero value", empty)
	}
}

// Legacy close-only labels and unknown barriers are not triple-barrier
// labels: they never move the EV or the rates.
func TestMetrics_TripleBarrierIgnoresLegacyLabels(t *testing.T) {
	legacy := domain.LabeledSample{
		Direction: domain.JevDirectionShort, Confidence: 0.75, HorizonMinutes: 5,
		FutureReturn: -1, WasDirectionCorrect: true, LabelVersion: domain.CalibrationLabelVersionClose,
	}
	m := calibration.Metrics([]domain.LabeledSample{legacy, barrierSample(0.75, domain.BarrierHitTakeProfit, 25)})

	if m.SampleCount != 2 {
		t.Fatalf("SampleCount = %d, want 2 (legacy labels still count toward accuracy)", m.SampleCount)
	}
	if b := m.Barrier; b.SampleCount != 1 || b.TPCount != 1 || !near(b.TPRate, 1) || !near(b.EVNetBps, 25) {
		t.Fatalf("Barrier = %+v, want only the triple-barrier label (1 tp, ev 25)", b)
	}

	if only := calibration.Metrics([]domain.LabeledSample{legacy}); only.Barrier != (domain.BarrierStats{}) {
		t.Fatalf("Barrier over legacy labels only = %+v, want zero value", only.Barrier)
	}
	if none := calibration.Metrics(nil); none.Barrier != (domain.BarrierStats{}) {
		t.Fatalf("Barrier over no samples = %+v, want zero value", none.Barrier)
	}
}

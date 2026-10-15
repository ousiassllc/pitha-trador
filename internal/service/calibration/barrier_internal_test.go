package calibration

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// testBarriers: TP 30, SL 30, cost 10 -> TP at +40 bps gross, SL at -20 bps.
func testBarriers() barriersettings.Barriers {
	return barriersettings.Barriers{TakeProfitBps: 30, StopLossBps: 30, Timeout: 10 * time.Minute, CostBps: 10}
}

func flat(gross ...float64) []barrierPoint {
	points := make([]barrierPoint, len(gross))
	for i, g := range gross {
		points[i] = barrierPoint{Favorable: g, Adverse: g, Close: g}
	}
	return points
}

func TestFirstBarrier(t *testing.T) {
	for name, tc := range map[string]struct {
		points    []barrierPoint
		barriers  barriersettings.Barriers
		wantHit   string
		wantIndex int
		wantNet   float64
	}{
		"take-profit first": {
			points: flat(5, 41, -50), barriers: testBarriers(),
			wantHit: domain.BarrierHitTakeProfit, wantIndex: 1, wantNet: 30,
		},
		"stop-loss first": {
			points: flat(-5, -21, 100), barriers: testBarriers(),
			wantHit: domain.BarrierHitStopLoss, wantIndex: 1, wantNet: -30,
		},
		"take-profit threshold is inclusive": {
			points: flat(40), barriers: testBarriers(),
			wantHit: domain.BarrierHitTakeProfit, wantIndex: 0, wantNet: 30,
		},
		"stop-loss threshold is inclusive": {
			points: flat(-20), barriers: testBarriers(),
			wantHit: domain.BarrierHitStopLoss, wantIndex: 0, wantNet: -30,
		},
		"just inside both barriers times out": {
			points: flat(39.9, -19.9, 5), barriers: testBarriers(),
			wantHit: domain.BarrierHitTimeout, wantIndex: 2, wantNet: -5,
		},
		"timeout is graded by net return, not flat = wrong": {
			points: flat(10, 25), barriers: testBarriers(),
			wantHit: domain.BarrierHitTimeout, wantIndex: 1, wantNet: 15,
		},
		"a bar touching both barriers is a stop-loss": {
			points:   []barrierPoint{{Favorable: 60, Adverse: -30, Close: 0}},
			barriers: testBarriers(),
			wantHit:  domain.BarrierHitStopLoss, wantIndex: 0, wantNet: -30,
		},
		"a later bar touching both is a stop-loss": {
			points:   append(flat(0), barrierPoint{Favorable: 45, Adverse: -25, Close: 10}),
			barriers: testBarriers(),
			wantHit:  domain.BarrierHitStopLoss, wantIndex: 1, wantNet: -30,
		},
		"zero cost: barriers are the raw widths": {
			points:   flat(29, 30),
			barriers: barriersettings.Barriers{TakeProfitBps: 30, StopLossBps: 30, Timeout: time.Minute},
			wantHit:  domain.BarrierHitTakeProfit, wantIndex: 1, wantNet: 30,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := firstBarrier(tc.points, tc.barriers)
			if got.Hit != tc.wantHit || got.Index != tc.wantIndex || got.NetReturnBps != tc.wantNet {
				t.Fatalf("firstBarrier = %+v, want hit %s at %d with net %v", got, tc.wantHit, tc.wantIndex, tc.wantNet)
			}
		})
	}
}

func TestGrossReturnBps(t *testing.T) {
	if got := grossReturnBps(1000, 1004, 1); got < 39.999 || got > 40.001 {
		t.Fatalf("LONG +0.4%% = %v bps, want 40", got)
	}
	if got := grossReturnBps(1000, 1004, -1); got > -39.999 || got < -40.001 {
		t.Fatalf("SHORT with price +0.4%% = %v bps, want -40", got)
	}
}

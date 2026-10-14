package calibration_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

func trade(arm, regime string, pnl, returnPct float64) marketregime.Trade {
	return marketregime.Trade{ABArm: arm, Regime: regime, RealizedPnL: pnl, ReturnPct: returnPct}
}

// issue #755: each A/B arm's win rate and cost-adjusted EV are reported,
// overall and per regime.
func TestABArms_SplitsByArmAndRegime(t *testing.T) {
	arms := calibration.ABArms([]marketregime.Trade{
		trade(marketregime.ArmMomentum, marketregime.Trend, 1000, 0.2),
		trade(marketregime.ArmMomentum, marketregime.Trend, -500, -0.1),
		trade(marketregime.ArmMomentum, marketregime.MeanRevert, -800, -0.3),
		trade(marketregime.ArmRegime, marketregime.MeanRevert, 600, 0.15),
		trade(marketregime.ArmRegime, marketregime.MeanRevert, 400, 0.05),
		trade(marketregime.ArmRegime, marketregime.Trend, 0, 0),
	})

	if len(arms) != 2 || arms[0].Arm != marketregime.ArmMomentum || arms[1].Arm != marketregime.ArmRegime {
		t.Fatalf("arms = %+v, want momentum then regime", arms)
	}
	m, r := arms[0], arms[1]
	if m.TradeCount != 3 || !near(m.WinRate, 1.0/3) || !near(m.TotalPnL, -300) {
		t.Errorf("momentum = %+v, want 3 trades, win 1/3, pnl -300", m.RegimeStats)
	}
	// EV: mean return (0.2 - 0.1 - 0.3)/3 % = -0.0667% = -6.67 bps.
	if !near(m.EVNetBps, (0.2-0.1-0.3)/3*100) {
		t.Errorf("momentum EV = %v bps, want %v", m.EVNetBps, (0.2-0.1-0.3)/3*100)
	}
	if r.TradeCount != 3 || !near(r.WinRate, 2.0/3) || !near(r.TotalPnL, 1000) || !near(r.EVNetBps, (0.15+0.05)/3*100) {
		t.Errorf("regime = %+v, want 3 trades, win 2/3, pnl 1000, EV %v bps", r.RegimeStats, (0.15+0.05)/3*100)
	}

	// Regimes come in the fixed order and only when they have trades.
	if len(m.ByRegime) != 2 || m.ByRegime[0].Regime != marketregime.Trend || m.ByRegime[1].Regime != marketregime.MeanRevert {
		t.Fatalf("momentum regimes = %+v, want trend then mean_revert", m.ByRegime)
	}
	if mr := m.ByRegime[1]; mr.TradeCount != 1 || mr.WinRate != 0 || !near(mr.EVNetBps, -30) {
		t.Errorf("momentum mean_revert = %+v, want 1 trade, win 0, EV -30 bps", mr)
	}
	if len(r.ByRegime) != 2 || r.ByRegime[0].Regime != marketregime.Trend || r.ByRegime[1].Regime != marketregime.MeanRevert {
		t.Fatalf("regime-arm regimes = %+v, want trend then mean_revert", r.ByRegime)
	}
	// A flat (zero PnL) trade is not a win.
	if rt := r.ByRegime[0]; rt.TradeCount != 1 || rt.WinRate != 0 {
		t.Errorf("regime-arm trend = %+v, want 1 trade, win 0", rt)
	}
}

func TestABArms_EmptyAndUnknownArms(t *testing.T) {
	arms := calibration.ABArms(nil)
	if len(arms) != 2 || arms[0].Arm != marketregime.ArmMomentum || arms[1].Arm != marketregime.ArmRegime {
		t.Fatalf("arms = %+v, want both arms reported", arms)
	}
	for _, a := range arms {
		if a.TradeCount != 0 || a.WinRate != 0 || a.EVNetBps != 0 || a.TotalPnL != 0 || len(a.ByRegime) != 0 {
			t.Errorf("empty arm %+v, want all zero", a)
		}
	}

	arms = calibration.ABArms([]marketregime.Trade{trade("other", marketregime.Trend, 100, 1)})
	if arms[0].TradeCount != 0 || arms[1].TradeCount != 0 {
		t.Errorf("a trade of an unknown arm was counted: %+v", arms)
	}
}

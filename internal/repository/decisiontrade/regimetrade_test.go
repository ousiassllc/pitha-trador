package decisiontrade_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// issue #755: closed positions are traced back to the A/B arm and market regime
// of the decision that opened them; a decision without an arm (made before the
// regime was assessed) and a manual entry are excluded, and a decision whose
// own direction is NONE still counts (a VWAP reversion trades against it).
func TestRepository_ListRegimeTrades(t *testing.T) {
	db := newTestDB(t)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	decisions := judgement.NewDecisionRepository(db)
	seed := &repositoryDB{
		signals: trading.NewSignalRepository(db), orders: trading.NewOrderRepository(db),
		positions: trading.NewPositionRepository(db),
	}
	base := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)

	insert := func(at time.Time, direction string, a marketregime.Assessment) domain.JevDecision {
		t.Helper()
		saved, err := decisions.Insert(context.Background(), domain.JevDecision{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: at, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
			Direction: &direction, ModelID: "test-model", MarketRegime: a,
		})
		if err != nil {
			t.Fatalf("insert decision: %v", err)
		}
		return saved
	}

	reversion := insert(base, domain.JevDirectionNone, marketregime.Assessment{Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime})
	openDecisionTrade(t, seed, reversion, true, 500)
	momentum := insert(base.Add(time.Hour), domain.JevDirectionLong, marketregime.Assessment{Regime: marketregime.Trend, ABArm: marketregime.ArmMomentum})
	openDecisionTrade(t, seed, momentum, true, -200)
	legacy := insert(base.Add(2*time.Hour), domain.JevDirectionLong, marketregime.Assessment{})
	openDecisionTrade(t, seed, legacy, true, 900)
	manual := insert(base.Add(3*time.Hour), domain.JevDirectionLong, marketregime.Assessment{Regime: marketregime.Trend, ABArm: marketregime.ArmMomentum})
	openDecisionTrade(t, seed, manual, false, 700)

	got, err := decisiontrade.New(db).ListRegimeTrades(context.Background())
	if err != nil {
		t.Fatalf("ListRegimeTrades: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRegimeTrades() = %+v, want the two signal-linked trades that carry an arm", got)
	}
	byArm := map[string]marketregime.Trade{}
	for _, tr := range got {
		byArm[tr.ABArm] = tr
	}
	// Notional = 1000 * 100, so 500 JPY is +0.5% and -200 JPY is -0.2%.
	if r := byArm[marketregime.ArmRegime]; r.Regime != marketregime.MeanRevert || r.RealizedPnL != 500 || r.ReturnPct != 0.5 {
		t.Errorf("regime-arm trade = %+v, want mean_revert, 500 JPY, +0.5%%", r)
	}
	if m := byArm[marketregime.ArmMomentum]; m.Regime != marketregime.Trend || m.RealizedPnL != -200 || m.ReturnPct != -0.2 {
		t.Errorf("momentum-arm trade = %+v, want trend, -200 JPY, -0.2%%", m)
	}
}

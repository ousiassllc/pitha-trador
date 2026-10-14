package judgement_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

// issue #755: the market regime, A/B arm and their evidence are stored with
// every decision and read back.
func TestDecisionRepository_RegimeAssessmentRoundTrips(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	vol, ac, z := 3.5, -0.4, 2.6

	created, err := repo.Insert(ctx, domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC),
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "r1", StateJSON: `{}`, QuestionVersion: "trader-v1", ResponseJSON: `{}`,
		MarketRegime: marketregime.Assessment{
			Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VolatilityBps: &vol, Autocorr: &ac, VWAPZ: &z,
		},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	r := got.MarketRegime
	if r.Regime != marketregime.MeanRevert || r.ABArm != marketregime.ArmRegime {
		t.Errorf("regime/arm = %q/%q, want mean_revert/regime", r.Regime, r.ABArm)
	}
	if r.VolatilityBps == nil || *r.VolatilityBps != vol || r.Autocorr == nil || *r.Autocorr != ac || r.VWAPZ == nil || *r.VWAPZ != z {
		t.Errorf("evidence = %v/%v/%v, want %v/%v/%v", r.VolatilityBps, r.Autocorr, r.VWAPZ, vol, ac, z)
	}
}

// A decision made without an assessor has no regime: it reads back as the zero
// assessment, not as an empty-string regime.
func TestDecisionRepository_UnassessedDecisionHasNoRegime(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()

	created, err := repo.Insert(ctx, domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: time.Date(2026, 10, 14, 1, 1, 0, 0, time.UTC),
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "r2", StateJSON: `{}`, QuestionVersion: "trader-v1", ResponseJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MarketRegime != (marketregime.Assessment{}) {
		t.Errorf("MarketRegime = %+v, want the zero assessment", got.MarketRegime)
	}
}

// The schema rejects a regime or arm outside the known values.
func TestDecisionRepository_RejectsUnknownRegime(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)

	_, err := repo.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: time.Date(2026, 10, 14, 1, 2, 0, 0, time.UTC),
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "r3", StateJSON: `{}`, QuestionVersion: "trader-v1", ResponseJSON: `{}`,
		MarketRegime: marketregime.Assessment{Regime: "sideways", ABArm: marketregime.ArmMomentum},
	})
	if err == nil {
		t.Fatal("Insert with an unknown regime succeeded, want a CHECK constraint error")
	}
}

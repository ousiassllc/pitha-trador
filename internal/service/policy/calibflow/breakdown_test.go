package calibflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// riskRejecting is the Risk Engine of a test: it rejects with reason, or passes
// when reason is empty.
type riskRejecting struct{ reason string }

func (r *riskRejecting) Check(context.Context, int64, string) (bool, string) {
	return r.reason == "", r.reason
}

// Issue #881: a decision judged on the all-versions total carries a note after
// its reject reason (decisionnote). risk_rejections' breakdown (paperresultsmcp
// query, not changed by that issue) takes the code of a reason from its head, so
// a noted reason must stay in its own bucket, never in "other" or in a bucket of
// its own that includes the note. Run through the real breakdown query.
func TestNotedRejectReasonsKeepTheirBreakdownBucket(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	d, err := judgement.NewDecisionRepository(db).Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: now(), DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "breakdown", StateJSON: "{}", QuestionVersion: "trader-v4", ResponseJSON: "{}", ModelID: "m",
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}

	risk := &riskRejecting{}
	engine := policy.NewEngine(thresholds(), risk, trading.NewSignalRepository(db),
		policytest.Option(policytest.Models{}, &policytest.Predictions{}), policy.WithPaperBootstrap(metalabelsettings.ModePaper, bootstrapOn{}))

	input := func() policy.Input {
		return policytest.WithInputs(policy.Input{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: now().Add(-10 * time.Second),
			Decision: &domain.JevDecision{
				ID: d.ID, Direction: ptr(domain.JevDirectionLong), EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.68),
				ContinuationProbability: ptr(0.60), ToxicFlow: ptr(0.35), LiquidityStressed: ptr(0.25),
			},
			SpreadBps: ptr(10.0), EntryPriceReference: ptr(2110.5), Calibrated: true,
			// Paper's all-versions fallback: every decision below is noted.
			CalibrationDetail: "live version trader-v4 has fewer than 3 labeled samples in the confidence bucket",
		})
	}
	evaluate := func(name string, mutate func(*policy.Input), riskReason string) domain.TradeSignal {
		t.Helper()
		in := input()
		mutate(&in)
		risk.reason = riskReason
		sig, err := engine.Evaluate(ctx, in)
		if err != nil {
			t.Fatalf("%s: Evaluate: %v", name, err)
		}
		if sig.RejectReason == nil || !strings.Contains(*sig.RejectReason, "calibration_all_versions: judged on all-versions total") {
			t.Fatalf("%s: reject_reason = %q, want the all-versions note", name, reasonText(sig))
		}
		return sig
	}
	// Reasons that are a bare code, a code with detail, and a Risk code with and
	// without detail; the last one is a LONG that passes (noted, risk_passed = 1).
	evaluate("special quote", func(in *policy.Input) { in.SpecialQuote = true }, "")
	evaluate("missing data", func(in *policy.Input) { in.SpreadBps = nil }, "")
	evaluate("missing data again", func(in *policy.Input) { in.Decision.EntryQuality = nil }, "")
	evaluate("price limit", func(in *policy.Input) { in.PriceLimit = domain.PriceLimitUp }, "")
	evaluate("risk code", func(*policy.Input) {}, "system_paused")
	evaluate("risk code with detail", func(*policy.Input) {}, "max_open_positions: count=3 max=3")
	if sig := evaluate("passed", func(*policy.Input) {}, ""); !sig.RiskPassed {
		t.Fatalf("a signal that clears every rule: %+v, want risk_passed", sig)
	}

	got, err := query.Rejections(ctx, db, query.Range{From: now().Add(-time.Hour), To: now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("Rejections: %v", err)
	}
	if got.RiskPassedCount != 1 || got.OtherRejectCount != 4 {
		t.Fatalf("risk_passed_count = %d, other_reject_count = %d, want 1, 4", got.RiskPassedCount, got.OtherRejectCount)
	}
	wantOther := map[string]int{"special_quote": 1, "missing_data": 2, "price_limit": 1}
	if len(got.OtherRejectBreakdown) != len(wantOther) {
		t.Errorf("other_reject_breakdown = %v, want %v (nothing in other)", got.OtherRejectBreakdown, wantOther)
	}
	for _, b := range got.OtherRejectBreakdown {
		if wantOther[b.Reason] != b.Count {
			t.Errorf("other_reject_breakdown %q = %d, want %v", b.Reason, b.Count, wantOther)
		}
	}
	wantRisk := map[string]int{"system_paused": 1, "max_open_positions": 1}
	if len(got.Buckets) != len(wantRisk) {
		t.Errorf("risk buckets = %v, want %v", got.Buckets, wantRisk)
	}
	for _, b := range got.Buckets {
		if wantRisk[b.Reason] != b.Count {
			t.Errorf("risk bucket %q = %d, want %v", b.Reason, b.Count, wantRisk)
		}
	}
}

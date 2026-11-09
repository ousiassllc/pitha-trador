package eligibility_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest/manifesttest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

// good is a LONG decision that clears every stage under the test rules.
func good(id int64) eligibility.Input {
	return eligibility.Input{
		ID: id, Labeled: true,
		Policy: policy.Input{
			InstrumentID: id, SpreadBps: f(3), Turnover5mJPY: f(1e7),
			Decision: &domain.JevDecision{
				Direction: s(domain.JevDirectionLong), Confidence: f(0.9), EntryQuality: s(domain.JevEntryQualityStrong),
				ContinuationProbability: f(0.9), ToxicFlow: f(0.1), LiquidityStressed: f(0.1),
			},
		},
		Features: metalabel.Features{OFIShort: f(1), OFIMid: f(1), OFILong: f(1), MicropriceDevBps: f(1), SpreadBps: f(3), Return1m: f(0.001), JevScore: f(0.5), TDnetScore: f(0)},
	}
}

func eval(t *testing.T, in eligibility.Input) eligibility.Result {
	t.Helper()
	e, err := eligibility.New(manifesttest.Valid().Rules, true)
	if err != nil {
		t.Fatal(err)
	}
	return e.Evaluate(context.Background(), in)
}

func stages(r eligibility.Result) (off, on metaablation.Stage) { return r.QOff.Stage, r.QOn.Stage }

func TestFirstRejectingStageForQOffAndQOn(t *testing.T) {
	type want struct{ off, on metaablation.Stage }
	for name, tc := range map[string]struct {
		mutate func(*eligibility.Input)
		want   want
		reason [2]string
	}{
		"clean pass":                     {func(*eligibility.Input) {}, want{0, 0}, [2]string{}},
		"quality fair passes only Q off": {func(in *eligibility.Input) { in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityFair) }, want{0, metaablation.StageQuality}, [2]string{"", policy.ReasonEntryQualityBelowThreshold}},
		"quality good passes both":       {func(in *eligibility.Input) { in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityGood) }, want{0, 0}, [2]string{}},
		"quality poor + toxic: Q first": {func(in *eligibility.Input) {
			in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityPoor)
			in.Policy.Decision.ToxicFlow = f(0.9)
		}, want{metaablation.StageFlowGuards, metaablation.StageQuality}, [2]string{policy.ReasonToxicFlowAboveThreshold, policy.ReasonEntryQualityBelowThreshold}},
		"continuation first of the guards": {func(in *eligibility.Input) {
			in.Policy.Decision.ContinuationProbability = f(0.1)
			in.Policy.Decision.ToxicFlow = f(0.9)
			in.Policy.Decision.LiquidityStressed = f(0.9)
		}, want{metaablation.StageFlowGuards, metaablation.StageFlowGuards}, [2]string{policy.ReasonContinuationProbabilityBelowThreshold, policy.ReasonContinuationProbabilityBelowThreshold}},
		"toxic before liquidity": {func(in *eligibility.Input) {
			in.Policy.Decision.ToxicFlow = f(0.9)
			in.Policy.Decision.LiquidityStressed = f(0.9)
		}, want{metaablation.StageFlowGuards, metaablation.StageFlowGuards}, [2]string{policy.ReasonToxicFlowAboveThreshold, policy.ReasonToxicFlowAboveThreshold}},
		"liquidity stressed alone": {func(in *eligibility.Input) { in.Policy.Decision.LiquidityStressed = f(0.9) }, want{metaablation.StageFlowGuards, metaablation.StageFlowGuards}, [2]string{policy.ReasonLiquidityStressedAboveThreshold, policy.ReasonLiquidityStressedAboveThreshold}},
		"eligibility beats quality and guards": {func(in *eligibility.Input) {
			in.Policy.SpreadBps = f(50)
			in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityPoor)
			in.Policy.Decision.ToxicFlow = f(0.9)
		}, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonSpreadTooWide, policy.ReasonSpreadTooWide}},
		"thin liquidity":      {func(in *eligibility.Input) { in.Policy.Turnover5mJPY = f(10) }, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonThinLiquidity, policy.ReasonThinLiquidity}},
		"special quote":       {func(in *eligibility.Input) { in.Policy.SpecialQuote = true }, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonSpecialQuote, policy.ReasonSpecialQuote}},
		"price limit":         {func(in *eligibility.Input) { in.Policy.PriceLimit = domain.PriceLimitUp }, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonPriceLimit, policy.ReasonPriceLimit}},
		"api error":           {func(in *eligibility.Input) { in.Policy.APIErr = context.DeadlineExceeded; in.Policy.Decision = nil }, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonAPIError, policy.ReasonAPIError}},
		"missing guard value": {func(in *eligibility.Input) { in.Policy.Decision.ToxicFlow = nil }, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonMissingData, policy.ReasonMissingData}},
		"short not lendable": {func(in *eligibility.Input) {
			in.Policy.Decision.Direction = s(domain.JevDirectionShort)
			in.Policy.Lendable = new(bool)
		}, want{metaablation.StageEligibility, metaablation.StageEligibility}, [2]string{policy.ReasonNotLendable, policy.ReasonNotLendable}},
		"jev none is not a candidate": {func(in *eligibility.Input) { in.Policy.Decision.Direction = s(domain.JevDirectionNone) }, want{metaablation.StageDecision, metaablation.StageDecision}, [2]string{"no_candidate", "no_candidate"}},
		"no label":                    {func(in *eligibility.Input) { in.Labeled = false }, want{metaablation.StageLabeled, metaablation.StageLabeled}, [2]string{"no_label", "no_label"}},
	} {
		t.Run(name, func(t *testing.T) {
			in := good(1)
			tc.mutate(&in)
			r := eval(t, in)
			if r.Unknown != "" {
				t.Fatalf("unknown: %s", r.Unknown)
			}
			if off, on := stages(r); off != tc.want.off || on != tc.want.on {
				t.Fatalf("stages off/on = %v/%v, want %v/%v (%+v)", off, on, tc.want.off, tc.want.on, r)
			}
			if tc.reason != [2]string{} && (r.QOff.Reason != tc.reason[0] || r.QOn.Reason != tc.reason[1]) {
				t.Errorf("reasons = %q/%q, want %q/%q", r.QOff.Reason, r.QOn.Reason, tc.reason[0], tc.reason[1])
			}
		})
	}
}

// TestQRejectedRowIsNotEligibleByItsStoredReasonAlone: the row the Q cut
// rejected first (so its toxic_flow guard never ran live) is re-evaluated, and
// found to fail the guard that Q off evaluates.
func TestQRejectedRowHasItsLaterGuardsEvaluated(t *testing.T) {
	in := good(1)
	in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityFair)
	in.Policy.Decision.ToxicFlow = f(0.9)
	r := eval(t, in)
	if !r.Eligible() {
		t.Fatal("the row passes the eligibility conditions and is a common candidate")
	}
	if r.QOn.Stage != metaablation.StageQuality || r.QOff.Stage != metaablation.StageFlowGuards || r.QOff.Reason != policy.ReasonToxicFlowAboveThreshold {
		t.Fatalf("got %+v", r)
	}
}

func TestEligibleIsCommonToBothQSettings(t *testing.T) {
	for _, mutate := range []func(*eligibility.Input){
		func(*eligibility.Input) {},
		func(in *eligibility.Input) { in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityPoor) },
		func(in *eligibility.Input) { in.Policy.Decision.ToxicFlow = f(0.9) },
	} {
		in := good(1)
		mutate(&in)
		if r := eval(t, in); !r.Eligible() {
			t.Errorf("not eligible: %+v", r)
		}
	}
	in := good(1)
	in.Policy.SpreadBps = f(50)
	if eval(t, in).Eligible() {
		t.Error("a spread-rejected row is eligible")
	}
}

func TestRowsThatCannotBeRecomputedAreUnknown(t *testing.T) {
	missingFeature := good(1)
	missingFeature.Features.Return1m = nil
	if r := eval(t, missingFeature); r.Unknown != eligibility.UnknownFeaturesIncomplete || r.Eligible() {
		t.Errorf("missing feature: %+v", r)
	}
	noMarket := good(2)
	noMarket.Unrecoverable = "market_data_unavailable"
	if r := eval(t, noMarket); r.Unknown != "market_data_unavailable" || r.Eligible() {
		t.Errorf("unrecoverable: %+v", r)
	}
	// An unknown row is in neither Q setting's stage counts.
	for _, qOn := range []bool{false, true} {
		fn := eligibility.BuildFunnel([]eligibility.Result{eval(t, missingFeature), eval(t, noMarket), eval(t, good(3))}, qOn)
		if fn.Rows != 3 || fn.Unknown != 2 || fn.ReachedMeta != 1 || fn.UnknownReasons["market_data_unavailable"] != 1 {
			t.Errorf("qOn=%v funnel %+v", qOn, fn)
		}
	}
}

func reversion(in *eligibility.Input, z *float64) {
	in.Policy.Decision.MarketRegime = marketregime.Assessment{Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VWAPZ: z}
}

func TestVWAPReversionIsNotSubjectToQ(t *testing.T) {
	in := good(1)
	in.Policy.Decision.EntryQuality = s(domain.JevEntryQualityPoor)
	reversion(&in, f(3)) // far above VWAP: SHORT, whatever Jev said
	r := eval(t, in)
	if !r.Reversion || r.QOff.Stage != 0 || r.QOn.Stage != 0 || r.Side != domain.JevDirectionShort {
		t.Fatalf("reversion: %+v", r)
	}
	fn := eligibility.BuildFunnel([]eligibility.Result{r, eval(t, good(2))}, true)
	if fn.ReachedMeta != 1 || fn.ReversionReachedMeta != 1 {
		t.Errorf("reversion counted with the momentum candidates: %+v", fn)
	}
	// The flow guards still apply to it.
	toxic := good(3)
	toxic.Policy.Decision.ToxicFlow = f(0.9)
	reversion(&toxic, f(3))
	if r := eval(t, toxic); r.QOn.Stage != metaablation.StageFlowGuards || r.QOff.Stage != metaablation.StageFlowGuards {
		t.Errorf("reversion with a toxic flow: %+v", r)
	}
	// VWAP rule conditions are eligibility conditions.
	unavailable := good(4)
	reversion(&unavailable, nil)
	if r := eval(t, unavailable); r.QOff.Stage != metaablation.StageEligibility || r.QOff.Reason != policy.ReasonVWAPZUnavailable {
		t.Errorf("vwap_z unavailable: %+v", r)
	}
	notExtreme := good(5)
	reversion(&notExtreme, f(0.1))
	if r := eval(t, notExtreme); r.QOff.Reason != policy.ReasonVWAPNotExtreme {
		t.Errorf("vwap not extreme: %+v", r)
	}
	// Jev's NONE direction is still a candidate under the reversion rule.
	none := good(6)
	none.Policy.Decision.Direction = s(domain.JevDirectionNone)
	reversion(&none, f(-3))
	if r := eval(t, none); r.QOff.Stage != 0 || r.Side != domain.JevDirectionLong {
		t.Errorf("reversion with Jev NONE: %+v", r)
	}
}

func TestEvaluatorUsesNoConfidenceThreshold(t *testing.T) {
	in := good(1)
	in.Policy.Decision.Confidence = f(0.01) // far below any min_probability
	if r := eval(t, in); r.QOn.Stage != 0 || r.QOff.Stage != 0 {
		t.Fatalf("the old confidence threshold was applied: %+v", r)
	}
}

func TestNewRefusesAnotherQualityCut(t *testing.T) {
	r := manifesttest.Valid().Rules
	r.QualityCut = "strong"
	if _, err := eligibility.New(r, true); err == nil {
		t.Fatal("a quality cut other than good was accepted")
	}
}

func TestLabelRequirementIsOptional(t *testing.T) {
	e, _ := eligibility.New(manifesttest.Valid().Rules, false)
	in := good(1)
	in.Labeled = false
	if r := e.Evaluate(context.Background(), in); r.QOff.Stage != 0 {
		t.Errorf("replay layer rows do not need a label: %+v", r)
	}
}

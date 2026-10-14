package regimeflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func ptr[T any](v T) *T { return &v }

func testThresholds() policy.Thresholds {
	side := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong",
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: side, Short: side}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}

// passingInput is an input for a Jev Trader decision that clears every guard at
// the boundary values (confidence 0.68, toxic_flow 0.35, ...).
func passingInput(direction string) policy.Input {
	return policytest.WithInputs(policy.Input{
		InstrumentID: 1, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Decision: &domain.JevDecision{
			ID: 7, Direction: ptr(direction), EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.68),
			ContinuationProbability: ptr(0.60), ToxicFlow: ptr(0.35), LiquidityStressed: ptr(0.25),
		},
		SpreadBps: ptr(10.0), EntryPriceReference: ptr(2110.5), Calibrated: true,
	})
}

// regimeInput is an input whose Jev decision (momentum direction jevDirection,
// every guard at its boundary) was made under a mean-reverting regime on the
// regime arm with the given VWAP deviation z-score (nil: unavailable).
func regimeInput(jevDirection string, z *float64) policy.Input {
	in := passingInput(jevDirection)
	in.Decision.MarketRegime = marketregime.Assessment{
		Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VWAPZ: z, VWAPZThreshold: 2,
	}
	return in
}

func reason(sig domain.TradeSignal) string {
	if sig.RejectReason == nil {
		return ""
	}
	return *sig.RejectReason
}

// issue #755: on the regime arm in a mean-reverting regime the candidate is the
// VWAP reversion - a SHORT above VWAP, a LONG below - whatever Jev's momentum
// direction is.
func TestDecide_VWAPReversionFadesTheDeviation(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	for name, tc := range map[string]struct {
		jev  string
		z    float64
		want string
	}{
		"above VWAP shorts although Jev says LONG": {domain.JevDirectionLong, 2.5, domain.JevDirectionShort},
		"below VWAP buys although Jev says SHORT":  {domain.JevDirectionShort, -2.5, domain.JevDirectionLong},
		"above VWAP shorts at the exact threshold": {domain.JevDirectionShort, 2, domain.JevDirectionShort},
		"below VWAP buys at the exact threshold":   {domain.JevDirectionLong, -2, domain.JevDirectionLong},
		"Jev NONE does not stop the reversion":     {domain.JevDirectionNone, 3, domain.JevDirectionShort},
	} {
		sig := e.Decide(context.Background(), regimeInput(tc.jev, &tc.z))
		if sig.Direction != tc.want || !sig.RiskPassed || sig.RejectReason != nil {
			t.Errorf("%s: signal = %q risk_passed=%v reason=%q, want %q", name, sig.Direction, sig.RiskPassed, reason(sig), tc.want)
		}
	}
}

func TestDecide_VWAPReversionNoneReasons(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	inside := 1.0

	for name, tc := range map[string]struct {
		in   policy.Input
		want string
	}{
		"deviation unavailable":   {regimeInput(domain.JevDirectionLong, nil), policy.ReasonVWAPZUnavailable},
		"deviation inside the z":  {regimeInput(domain.JevDirectionLong, &inside), policy.ReasonVWAPNotExtreme},
		"deviation just inside z": {regimeInput(domain.JevDirectionLong, ptr(1.99)), policy.ReasonVWAPNotExtreme},
	} {
		sig := e.Decide(context.Background(), tc.in)
		if sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reason(sig), tc.want) {
			t.Errorf("%s: signal = %q reason=%q, want NONE with reason %q", name, sig.Direction, reason(sig), tc.want)
		}
	}
}

func TestDecide_VWAPReversionKeepsTheHardGuards(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	toxic := regimeInput(domain.JevDirectionLong, ptr(3.0))
	toxic.Decision.ToxicFlow = ptr(0.36)
	if sig := e.Decide(context.Background(), toxic); sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reason(sig), policy.ReasonToxicFlowAboveThreshold) {
		t.Errorf("toxic flow: signal = %q reason=%q, want NONE toxic_flow", sig.Direction, reason(sig))
	}

	stressed := regimeInput(domain.JevDirectionLong, ptr(3.0))
	stressed.Decision.LiquidityStressed = ptr(0.26)
	if sig := e.Decide(context.Background(), stressed); sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reason(sig), policy.ReasonLiquidityStressedAboveThreshold) {
		t.Errorf("liquidity stress: signal = %q reason=%q, want NONE liquidity_stressed", sig.Direction, reason(sig))
	}

	// The reversion short needs a lendable stock like any short.
	notLendable := regimeInput(domain.JevDirectionLong, ptr(3.0))
	notLendable.Lendable = ptr(false)
	if sig := e.Decide(context.Background(), notLendable); sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reason(sig), policy.ReasonNotLendable) {
		t.Errorf("not lendable: signal = %q reason=%q, want NONE not_lendable", sig.Direction, reason(sig))
	}

	// The reversion side does not depend on Jev's own momentum-direction
	// guards: a weak entry_quality / continuation_probability does not stop it.
	weak := regimeInput(domain.JevDirectionLong, ptr(3.0))
	weak.Decision.EntryQuality = ptr(domain.JevEntryQualityPoor)
	weak.Decision.ContinuationProbability = ptr(0.1)
	weak.Decision.Confidence = ptr(0.2)
	if sig := e.Decide(context.Background(), weak); sig.Direction != domain.JevDirectionShort {
		t.Errorf("weak momentum guards: signal = %q reason=%q, want SHORT", sig.Direction, reason(sig))
	}
}

// The other arm and the other regimes keep the existing momentum rule.
func TestDecide_MomentumUnlessRegimeArmInMeanRevertingRegime(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	for name, a := range map[string]marketregime.Assessment{
		"momentum arm in a mean-reverting regime": {Regime: marketregime.MeanRevert, ABArm: marketregime.ArmMomentum, VWAPZ: ptr(3.0)},
		"regime arm in a trend":                   {Regime: marketregime.Trend, ABArm: marketregime.ArmRegime, VWAPZ: ptr(3.0)},
		"regime arm, regime unknown":              {Regime: marketregime.Unknown, ABArm: marketregime.ArmRegime, VWAPZ: ptr(3.0)},
		"not assessed":                            {},
	} {
		in := passingInput(domain.JevDirectionLong)
		in.Decision.MarketRegime = a
		if sig := e.Decide(context.Background(), in); sig.Direction != domain.JevDirectionLong || sig.Score == nil || *sig.Score != 0.68 {
			t.Errorf("%s: signal = %q score=%v, want Jev's LONG with its confidence", name, sig.Direction, sig.Score)
		}
	}
}

// A replayed decision carries no threshold (it is not stored): the shipped
// default applies.
func TestDecide_VWAPReversionDefaultsThresholdWhenUnset(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	in := regimeInput(domain.JevDirectionLong, ptr(2.1))
	in.Decision.MarketRegime.VWAPZThreshold = 0

	if sig := e.Decide(context.Background(), in); sig.Direction != domain.JevDirectionShort {
		t.Errorf("signal = %q, want SHORT at z 2.1 against the default threshold 2", sig.Direction)
	}
	in.Decision.MarketRegime.VWAPZ = ptr(1.9)
	if sig := e.Decide(context.Background(), in); sig.Direction != domain.JevDirectionNone {
		t.Errorf("signal = %q, want NONE at z 1.9 against the default threshold 2", sig.Direction)
	}
}

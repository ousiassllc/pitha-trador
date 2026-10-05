package policy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func TestEngine_Decide_UntradableBarIsNone(t *testing.T) {
	cases := []struct {
		name       string
		mut        func(*policy.Input)
		wantReason string
	}{
		{"special quote", func(in *policy.Input) { in.SpecialQuote = true }, policy.ReasonSpecialQuote},
		{"stop high", func(in *policy.Input) { in.PriceLimit = domain.PriceLimitUp }, policy.ReasonPriceLimit},
		{"stop low", func(in *policy.Input) { in.PriceLimit = domain.PriceLimitDown }, policy.ReasonPriceLimit},
	}
	e := policy.NewEngine(testThresholds(), nil, nil)
	for _, c := range cases {
		for _, dir := range []string{domain.JevDirectionLong, domain.JevDirectionShort} {
			t.Run(c.name+"/"+dir, func(t *testing.T) {
				in := passingInput(dir)
				c.mut(&in)
				sig := e.Decide(context.Background(), in)
				if sig.Direction != domain.JevDirectionNone || sig.RiskPassed {
					t.Fatalf("Direction=%q RiskPassed=%v, want none/false", sig.Direction, sig.RiskPassed)
				}
				if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, c.wantReason) {
					t.Fatalf("RejectReason = %v, want prefix %q", sig.RejectReason, c.wantReason)
				}
			})
		}
	}
}

// A non-貸借 instrument blocks SHORT only; unknown lendability (nil) blocks
// nothing, and LONG on a non-lendable instrument is fine.
func TestEngine_Decide_NotLendableBlocksOnlyShort(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	short := passingInput(domain.JevDirectionShort)
	short.Lendable = ptr(false)
	sig := e.Decide(context.Background(), short)
	if sig.Direction != domain.JevDirectionNone || sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonNotLendable) {
		t.Fatalf("short non-lendable: Direction=%q RejectReason=%v, want none/%s", sig.Direction, sig.RejectReason, policy.ReasonNotLendable)
	}

	for name, in := range map[string]policy.Input{
		"short lendable":    func() policy.Input { i := passingInput(domain.JevDirectionShort); i.Lendable = ptr(true); return i }(),
		"short unknown":     passingInput(domain.JevDirectionShort),
		"long non-lendable": func() policy.Input { i := passingInput(domain.JevDirectionLong); i.Lendable = ptr(false); return i }(),
	} {
		if got := e.Decide(context.Background(), in); got.Direction == domain.JevDirectionNone {
			t.Errorf("%s: Direction = none (%v), want a signal", name, got.RejectReason)
		}
	}
}

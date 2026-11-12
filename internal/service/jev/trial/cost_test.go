package trial_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestAnsweredCost(t *testing.T) {
	priced := jevtrialsettings.Arm{ListPriceUSDPerMtok: 0.192}
	unpriced := jevtrialsettings.Arm{}
	for name, tc := range map[string]struct {
		arm       jevtrialsettings.Arm
		resp      trialwire.Response
		bytes     int
		source    string
		usd       *float64
		countedBy float64
	}{
		"reported wins over the list price": {
			priced, trialwire.Response{ReportedCostUSD: new(0.0007), InputTokens: 1000}, 3000, jevtrial.CostReported, new(0.0007), 0.0007},
		"reported zero is a reported cost": {
			priced, trialwire.Response{ReportedCostUSD: new(0.0), InputTokens: 1000}, 3000, jevtrial.CostReported, new(0.0), 0},
		"list price over reported tokens": {
			priced, trialwire.Response{InputTokens: 1000}, 3000, jevtrial.CostListPriceEstimate, new(1000 * 0.192 / 1e6), 1000 * 0.192 / 1e6},
		"list price over estimated tokens": {
			priced, trialwire.Response{}, 3000, jevtrial.CostListPriceEstimate,
			new(float64(shadow.EstimateInputTokens(3000)) * 0.192 / 1e6), float64(shadow.EstimateInputTokens(3000)) * 0.192 / 1e6},
		"unknown counts the worst case, never 0": {
			unpriced, trialwire.Response{InputTokens: 1000}, 3000, jevtrial.CostUnknown, nil, 1000 * jevtrialsettings.WorstCaseUSDPerMtok / 1e6},
		"unknown without usage counts the estimated tokens": {
			unpriced, trialwire.Response{}, 3000, jevtrial.CostUnknown, nil,
			float64(shadow.EstimateInputTokens(3000)) * jevtrialsettings.WorstCaseUSDPerMtok / 1e6},
		"a negative reported cost is not trusted": {
			unpriced, trialwire.Response{ReportedCostUSD: new(-1.0), InputTokens: 1000}, 3000, jevtrial.CostUnknown, nil, 1000 * jevtrialsettings.WorstCaseUSDPerMtok / 1e6},
	} {
		t.Run(name, func(t *testing.T) {
			c := trial.AnsweredCost(tc.arm, tc.resp, tc.bytes)
			if c.Source != tc.source || (c.USD == nil) != (tc.usd == nil) || (tc.usd != nil && !near(*c.USD, *tc.usd)) || !near(c.CountedUSD, tc.countedBy) {
				t.Fatalf("cost = %v %v %v, want %v %v %v", c.Source, c.USD, c.CountedUSD, tc.source, tc.usd, tc.countedBy)
			}
			if c.Source == jevtrial.CostUnknown && (c.USD != nil || c.CountedUSD <= 0) {
				t.Fatalf("unknown must have NULL cost and a positive counted cost: %+v", c)
			}
		})
	}
}

func TestFailedCost(t *testing.T) {
	http := trial.FailedCost(true, 3000)
	if http.Source != jevtrial.CostUnknown || http.USD != nil || http.CountedUSD != 0 {
		t.Errorf("HTTP error answer = %+v, want unknown / NULL / counted 0 (documented: not billed)", http)
	}
	other := trial.FailedCost(false, 3000)
	want := float64(shadow.EstimateInputTokens(3000)) * jevtrialsettings.WorstCaseUSDPerMtok / 1e6
	if other.Source != jevtrial.CostUnknown || other.USD != nil || !near(other.CountedUSD, want) || want <= 0 {
		t.Errorf("a failure that may have been billed = %+v, want unknown / NULL / counted %v", other, want)
	}
}

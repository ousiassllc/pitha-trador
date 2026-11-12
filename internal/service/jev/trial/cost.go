package trial

import (
	"math"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// Cost is what one call is stored and counted as.
type Cost struct {
	// USD is the reported or list-price-estimated cost; nil when unknown. It is
	// never 0 for "unknown".
	USD *float64
	// Source is jevtrial.CostReported, CostListPriceEstimate or CostUnknown.
	Source string
	// CountedUSD is what the daily caps count for the call.
	CountedUSD float64
}

// AnsweredCost prices a call that returned an answer set. Order of preference:
//
//  1. usage.cost reported by the response: used as is (source reported).
//  2. Input tokens (the reported ones, else shadow.EstimateInputTokens of the
//     request's size) times the arm's list price per million tokens (source
//     list_price_estimate). Only input tokens are priced: the stage-1 models'
//     published prices are per input token and a reported cost wins anyway.
//  3. Neither: USD is nil and source unknown; the call is counted at
//     jevtrialsettings.WorstCaseUSDPerMtok so an unpriced arm cannot slip under
//     a cap as 0 USD.
func AnsweredCost(arm jevtrialsettings.Arm, resp trialwire.Response, requestBytes int) Cost {
	if c := resp.ReportedCostUSD; c != nil && *c >= 0 && !math.IsNaN(*c) && !math.IsInf(*c, 0) {
		return Cost{USD: new(*c), Source: jevtrial.CostReported, CountedUSD: *c}
	}
	tokens := resp.InputTokens
	if tokens <= 0 {
		tokens = shadow.EstimateInputTokens(requestBytes)
	}
	if arm.ListPriceUSDPerMtok > 0 {
		usd := float64(tokens) * arm.ListPriceUSDPerMtok / 1e6
		return Cost{USD: new(usd), Source: jevtrial.CostListPriceEstimate, CountedUSD: usd}
	}
	return Cost{Source: jevtrial.CostUnknown, CountedUSD: worstCaseUSD(tokens)}
}

// FailedCost counts a call that returned no answer set. The cost is always
// unknown (nil, never 0 USD).
//
// An HTTP error status means the vendor answered without running the model:
// that is assumed not to be billed (an assumption, not a measurement), so it
// counts 0 against the caps; the day's failure counter and the 402/403/429/529
// stops bound such calls instead. Any other failure (timeout, transport error,
// an answer that is not a valid answer set) may have been billed, so it counts
// at the worst-case price over the estimated input tokens of the request.
func FailedCost(vendorAnsweredWithError bool, requestBytes int) Cost {
	if vendorAnsweredWithError {
		return Cost{Source: jevtrial.CostUnknown}
	}
	return Cost{Source: jevtrial.CostUnknown, CountedUSD: worstCaseUSD(shadow.EstimateInputTokens(requestBytes))}
}

func worstCaseUSD(tokens int) float64 {
	return float64(tokens) * jevtrialsettings.WorstCaseUSDPerMtok / 1e6
}

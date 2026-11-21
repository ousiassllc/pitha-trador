package assist

import (
	"encoding/json"
	"fmt"
)

func encodeReview(review OpusReview) (bool, string, error) {
	data, err := json.Marshal(review)
	if err != nil {
		return false, "", fmt.Errorf("assist: opus: encode review: %w", err)
	}
	return review.Approved, string(data), nil
}

func deterministicRejectReason(sufficientSamples bool, cmp BacktestComparison, expectancyNotWorse, maxDrawdownWithinBudget bool) string {
	switch {
	case !sufficientSamples:
		return fmt.Sprintf("%s: shadow backtest needs at least %d trades per side over at least %d trading days, got baseline=%d candidate=%d over %d days; "+
			"a cut-off, not a proof of safety (FR-SELFIMPROVE-4)",
			ReasonInsufficientSamples, MinBacktestTrades, MinBacktestDays, cmp.BaselineTradeCount, cmp.CandidateTradeCount, cmp.BacktestDays)
	case !expectancyNotWorse && !maxDrawdownWithinBudget:
		return "shadow backtest Expectancy worsened and Max Drawdown exceeded the 10% relative budget (FR-SELFIMPROVE-4)"
	case !expectancyNotWorse:
		return "shadow backtest Expectancy worsened versus the current policy_version (FR-SELFIMPROVE-4)"
	default:
		return "shadow backtest Max Drawdown exceeded the 10% relative budget versus the current policy_version (FR-SELFIMPROVE-4)"
	}
}

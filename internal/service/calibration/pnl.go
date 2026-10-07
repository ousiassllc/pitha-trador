package calibration

import "github.com/ousiassllc/pitha-trador/internal/domain"

// WithTradePnL folds trades (closed positions traced back to the Jev
// trader decision that opened them) into metrics as the realized-PnL
// ground truth (functional.md FR-CAL-2 "confidence bucket別PnL", issue
// #711). Per confidence bucket: TradeCount, TotalPnL, AvgPnLPct and
// TradeWinRate (the share of trades with RealizedPnL > 0). Overall:
// TradeCount, and PnLBrierScore/PnLLogLoss, the same scores Metrics
// computes against the price-path ground truth, here with the trade's
// Confidence as the predicted probability and RealizedPnL > 0 as the
// outcome. RealizedPnL is net of fees and priced by the fill model's
// spread/slippage (FR-ENTRY-8), so this ground truth already includes
// those costs. A trade whose Confidence falls outside every bucket range
// is ignored, matching how Metrics treats samples. metrics.Buckets is
// modified in place.
func WithTradePnL(metrics domain.CalibrationMetrics, trades []domain.DecisionTrade) domain.CalibrationMetrics {
	returnSum := make([]float64, len(metrics.Buckets))
	winSum := make([]int, len(metrics.Buckets))
	var brierSum, logLossSum float64
	for _, trade := range trades {
		idx, ok := bucketIndex(domain.DefaultConfidenceBucketRanges, trade.Confidence)
		if !ok {
			continue
		}
		metrics.Buckets[idx].TradeCount++
		metrics.Buckets[idx].TotalPnL += trade.RealizedPnL
		returnSum[idx] += trade.ReturnPct
		outcome := 0.0
		if trade.RealizedPnL > 0 {
			winSum[idx]++
			outcome = 1.0
		}
		brier, logLoss := binaryScores(trade.Confidence, outcome)
		brierSum += brier
		logLossSum += logLoss
		metrics.TradeCount++
	}
	for i := range metrics.Buckets {
		if metrics.Buckets[i].TradeCount > 0 {
			n := float64(metrics.Buckets[i].TradeCount)
			metrics.Buckets[i].AvgPnLPct = returnSum[i] / n
			metrics.Buckets[i].TradeWinRate = float64(winSum[i]) / n
		}
	}
	if metrics.TradeCount > 0 {
		metrics.PnLBrierScore = brierSum / float64(metrics.TradeCount)
		metrics.PnLLogLoss = logLossSum / float64(metrics.TradeCount)
	}
	return metrics
}

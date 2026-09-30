package calibration

import "github.com/ousiassllc/pitha-trador/internal/domain"

// WithTradePnL folds trades (closed positions traced back to the Jev
// trader decision that opened them) into metrics' confidence buckets:
// each bucket's TradeCount, TotalPnL and AvgPnLPct (functional.md
// FR-CAL-2 "confidence bucket別PnL"). A trade whose Confidence falls
// outside every bucket range is ignored, matching how Metrics treats
// samples. metrics.Buckets is modified in place.
func WithTradePnL(metrics domain.CalibrationMetrics, trades []domain.DecisionTrade) domain.CalibrationMetrics {
	returnSum := make([]float64, len(metrics.Buckets))
	for _, trade := range trades {
		idx, ok := bucketIndex(domain.DefaultConfidenceBucketRanges, trade.Confidence)
		if !ok {
			continue
		}
		metrics.Buckets[idx].TradeCount++
		metrics.Buckets[idx].TotalPnL += trade.RealizedPnL
		returnSum[idx] += trade.ReturnPct
	}
	for i := range metrics.Buckets {
		if metrics.Buckets[i].TradeCount > 0 {
			metrics.Buckets[i].AvgPnLPct = returnSum[i] / float64(metrics.Buckets[i].TradeCount)
		}
	}
	return metrics
}

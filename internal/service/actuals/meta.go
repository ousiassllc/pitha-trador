package actuals

import (
	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// metaComparisons sets each model's OOS metrics beside the realized Paper
// results of the same model (issue #777 要件4): the trades entered on a
// decision the model scored, and the trades closed inside the model's OOS
// window. trades must be ordered by ClosedAt and already restricted to the
// evaluated range.
func metaComparisons(models []repo.MetaModel, trades []repo.Trade, capital float64, c Criteria) []MetaComparison {
	out := make([]MetaComparison, 0, len(models))
	for _, m := range models {
		var linked, window []repo.Trade
		for _, t := range trades {
			if t.MetaModelVersion == m.Version {
				linked = append(linked, t)
			}
			if !t.ClosedAt.Before(m.OOSStart) && t.ClosedAt.Before(m.OOSEnd) {
				window = append(window, t)
			}
		}
		mc := MetaComparison{
			Version: m.Version, Algorithm: m.Algorithm, Status: m.Status, LabelVersion: m.LabelVersion,
			TrainedAt: m.TrainedAt, OOSStart: m.OOSStart, OOSEnd: m.OOSEnd, OOSSamples: m.OOSSamples,
			OOSReliable: m.OOSSamples >= c.MinSample, OOSAUC: m.OOSAUC, OOSExpectedBps: m.OOSExpectedBps,
			Linked:      summarize(linked, capital, c),
			InOOSWindow: summarize(window, capital, c),
		}
		mc.PredictedEVBps, mc.EVGapBps = predictedVsRealized(linked, c)
		out = append(out, mc)
	}
	return out
}

// predictedVsRealized is the mean EV the model predicted for trades (those
// that carry a prediction) and the mean per-trade difference of the realized
// return (bps of the entry notional, as returnBps) to it.
func predictedVsRealized(trades []repo.Trade, c Criteria) (predicted, gap Metric) {
	var pred, diff []float64
	var days []int64
	for _, t := range trades {
		if t.MetaExpectedBps == nil {
			continue
		}
		pred = append(pred, *t.MetaExpectedBps)
		diff = append(diff, returnBps(t)-*t.MetaExpectedBps)
		days = append(days, jstDay(t.ClosedAt))
	}
	return meanMetric(pred, days, c), meanMetric(diff, days, c)
}

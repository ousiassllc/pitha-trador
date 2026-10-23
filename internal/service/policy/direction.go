package policy

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// maxRejectReasonBytes is trade_signals.reject_reason's declared width
// (VARCHAR(255)); SQLite does not enforce it, so reasonf does.
const maxRejectReasonBytes = 255

// decideDirection implements FR-POLICY-1〜3's decision tree, in order:
// API失敗 -> データ欠損/未評価 -> キャリブレーション対象外 -> スプレッド/
// 流動性 -> 特別気配/ストップ高安 -> Jevのdirection別（ショートは貸借銘柄
// のみ）の評価. It does not consult RiskChecker; decide applies that as the
// final gate.
//
// The last step depends on meta. A nil meta is the legacy rule (the backtest
// replay and the comparison log): Jev's confidence must clear
// policy.*.min_probability and its confidence bucket must be calibrated. A
// non-nil meta is the live rule (issue #765): the meta-labeling model's
// expected-value verdict decides, so Jev's confidence is no probability and
// the confidence-bucket calibration gate does not apply; verdict is the
// model's answer when the candidate reached it.
func (e *Engine) decideDirection(in Input, th Thresholds, meta *metaEntry) (direction string, score *float64, reason *string, verdict *metalabel.Decision) {
	none := func(reason *string) (string, *float64, *string, *metalabel.Decision) {
		return domain.JevDirectionNone, nil, reason, nil
	}
	if in.APIErr != nil {
		return none(reasonf("%s: %v", ReasonAPIError, in.APIErr))
	}
	if in.Decision == nil {
		return none(reasonf(ReasonMissingData))
	}
	if meta == nil && !in.Calibrated {
		return none(notCalibrated(in.CalibrationDetail))
	}
	if meta != nil && meta.cohort.Uncalibrated != "" {
		// Live does not use a model trained on another prompt cohort than the live
		// version's: it is as good as no calibration (issue #881).
		return none(notCalibrated(meta.cohort.Uncalibrated))
	}
	if in.SpreadBps == nil {
		return none(reasonf(ReasonMissingData))
	}
	if *in.SpreadBps > th.MaxSpreadBps {
		return none(reasonf("%s: spread_bps=%.2f > max=%.2f", ReasonSpreadTooWide, *in.SpreadBps, th.MaxSpreadBps))
	}
	if in.Turnover5mJPY != nil && *in.Turnover5mJPY < th.MinTurnover5mJPY {
		return none(reasonf("%s: turnover_5m_jpy=%.0f < min=%.0f", ReasonThinLiquidity, *in.Turnover5mJPY, th.MinTurnover5mJPY))
	}

	if in.SpecialQuote {
		return none(reasonf(ReasonSpecialQuote))
	}
	if in.PriceLimit != domain.PriceLimitNone {
		return none(reasonf("%s: stop_%s", ReasonPriceLimit, in.PriceLimit))
	}

	d := in.Decision
	if d.Direction == nil || d.EntryQuality == nil || d.Confidence == nil ||
		d.ContinuationProbability == nil || d.ToxicFlow == nil || d.LiquidityStressed == nil {
		return none(reasonf(ReasonMissingData))
	}

	side, thresholds, reversion, reject := chooseSide(in, th)
	if reject != nil {
		return none(reject)
	}
	if meta != nil {
		return e.decideMeta(side, thresholds, in, meta, reversion)
	}
	if reversion {
		// The reversion side is not Jev's momentum direction, so Jev's
		// confidence in it is no probability and only the hard guards apply.
		if reason := flowGuards(thresholds, d); reason != nil {
			return none(reason)
		}
		score := *d.Confidence
		return side, &score, nil, nil
	}
	direction, score, reason = e.evaluateThreshold(side, thresholds, d, true)
	return direction, score, reason, nil
}

// evaluateThreshold implements FR-POLICY-1 (direction ==
// domain.JevDirectionLong) / FR-POLICY-2 (direction ==
// domain.JevDirectionShort)'s compound AND condition: probability,
// entry_quality, continuation_probability, toxic_flow and
// liquidity_stressed must all clear t. Any single failure is NONE
// (functional.md FR-POLICY-3's generic "確信度不足"). withProbability is false
// for the live meta-labeling rule, which replaces the probability condition
// (Jev's confidence against min_probability) with the model's verdict and
// keeps the other four as hard guards.
func (e *Engine) evaluateThreshold(direction string, t config.PolicyDirectionThresholds, d *domain.JevDecision, withProbability bool) (string, *float64, *string) {
	probability := *d.Confidence
	if withProbability && probability < t.MinProbability {
		return domain.JevDirectionNone, nil, reasonf("%s: probability=%.4f < min=%.4f", ReasonProbabilityBelowThreshold, probability, t.MinProbability)
	}
	if entryQualityRank[*d.EntryQuality] < entryQualityRank[t.MinEntryQuality] {
		return domain.JevDirectionNone, nil, reasonf("%s: entry_quality=%q < min=%q", ReasonEntryQualityBelowThreshold, *d.EntryQuality, t.MinEntryQuality)
	}
	if *d.ContinuationProbability < t.MinContinuationProbability {
		return domain.JevDirectionNone, nil, reasonf("%s: continuation_probability=%.4f < min=%.4f", ReasonContinuationProbabilityBelowThreshold, *d.ContinuationProbability, t.MinContinuationProbability)
	}
	if reason := flowGuards(t, d); reason != nil {
		return domain.JevDirectionNone, nil, reason
	}
	score := probability
	return direction, &score, nil
}

// flowGuards is the toxic-flow and liquidity-stress part of FR-POLICY-1/2's
// hard guards, shared by the momentum rule and the VWAP reversion rule (whose
// side is not Jev's, so entry_quality and continuation_probability, which
// describe Jev's momentum direction, do not apply to it). It returns the NONE
// reason, or nil when both clear t.
func flowGuards(t config.PolicyDirectionThresholds, d *domain.JevDecision) *string {
	if *d.ToxicFlow > t.MaxToxicFlow {
		return reasonf("%s: toxic_flow=%.4f > max=%.4f", ReasonToxicFlowAboveThreshold, *d.ToxicFlow, t.MaxToxicFlow)
	}
	if *d.LiquidityStressed > t.MaxLiquidityStressed {
		return reasonf("%s: liquidity_stressed=%.4f > max=%.4f", ReasonLiquidityStressedAboveThreshold, *d.LiquidityStressed, t.MaxLiquidityStressed)
	}
	return nil
}

// notCalibrated is the NONE reason of a decision that is not calibrated; detail
// says which sample count or model is the reason ("" for none).
func notCalibrated(detail string) *string {
	if detail == "" {
		return reasonf(ReasonNotCalibrated)
	}
	return reasonf("%s: %s", ReasonNotCalibrated, detail)
}

// reasonf formats a reject_reason string and returns it as *string
// (domain.TradeSignal.RejectReason's type). format may be a plain
// constant (no verbs).
func reasonf(format string, args ...any) *string {
	s := textutil.Truncate(fmt.Sprintf(format, args...), maxRejectReasonBytes)
	return &s
}

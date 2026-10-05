package policy

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// maxRejectReasonBytes is trade_signals.reject_reason's declared width
// (VARCHAR(255)); SQLite does not enforce it, so reasonf does.
const maxRejectReasonBytes = 255

// decideDirection implements FR-POLICY-1〜3's decision tree, in order:
// API失敗 -> データ欠損/未評価 -> キャリブレーション対象外 -> スプレッド/
// 流動性 -> 特別気配/ストップ高安 -> Jevのdirection別（ショートは貸借銘柄
// のみ）しきい値評価. It does not consult RiskChecker; Decide applies that
// as the final gate.
func (e *Engine) decideDirection(in Input, th Thresholds) (direction string, score *float64, reason *string) {
	if in.APIErr != nil {
		return domain.JevDirectionNone, nil, reasonf("%s: %v", ReasonAPIError, in.APIErr)
	}
	if in.Decision == nil {
		return domain.JevDirectionNone, nil, reasonf(ReasonMissingData)
	}
	if !in.Calibrated {
		return domain.JevDirectionNone, nil, reasonf(ReasonNotCalibrated)
	}
	if in.SpreadBps == nil {
		return domain.JevDirectionNone, nil, reasonf(ReasonMissingData)
	}
	if *in.SpreadBps > th.MaxSpreadBps {
		return domain.JevDirectionNone, nil, reasonf("%s: spread_bps=%.2f > max=%.2f", ReasonSpreadTooWide, *in.SpreadBps, th.MaxSpreadBps)
	}
	if in.Turnover5mJPY != nil && *in.Turnover5mJPY < th.MinTurnover5mJPY {
		return domain.JevDirectionNone, nil, reasonf("%s: turnover_5m_jpy=%.0f < min=%.0f", ReasonThinLiquidity, *in.Turnover5mJPY, th.MinTurnover5mJPY)
	}

	if in.SpecialQuote {
		return domain.JevDirectionNone, nil, reasonf(ReasonSpecialQuote)
	}
	if in.PriceLimit != domain.PriceLimitNone {
		return domain.JevDirectionNone, nil, reasonf("%s: stop_%s", ReasonPriceLimit, in.PriceLimit)
	}

	d := in.Decision
	if d.Direction == nil || d.EntryQuality == nil || d.Confidence == nil ||
		d.ContinuationProbability == nil || d.ToxicFlow == nil || d.LiquidityStressed == nil {
		return domain.JevDirectionNone, nil, reasonf(ReasonMissingData)
	}

	switch *d.Direction {
	case domain.JevDirectionLong:
		return e.evaluateThreshold(domain.JevDirectionLong, th.Policy.Long, d)
	case domain.JevDirectionShort:
		if in.Lendable != nil && !*in.Lendable {
			return domain.JevDirectionNone, nil, reasonf(ReasonNotLendable)
		}
		return e.evaluateThreshold(domain.JevDirectionShort, th.Policy.Short, d)
	default:
		return domain.JevDirectionNone, nil, reasonf(ReasonJevNone)
	}
}

// evaluateThreshold implements FR-POLICY-1 (direction ==
// domain.JevDirectionLong) / FR-POLICY-2 (direction ==
// domain.JevDirectionShort)'s compound AND condition: probability,
// entry_quality, continuation_probability, toxic_flow and
// liquidity_stressed must all clear t. Any single failure is NONE
// (functional.md FR-POLICY-3's generic "確信度不足").
func (e *Engine) evaluateThreshold(direction string, t config.PolicyDirectionThresholds, d *domain.JevDecision) (string, *float64, *string) {
	probability := *d.Confidence
	if probability < t.MinProbability {
		return domain.JevDirectionNone, nil, reasonf("%s: probability=%.4f < min=%.4f", ReasonProbabilityBelowThreshold, probability, t.MinProbability)
	}
	if entryQualityRank[*d.EntryQuality] < entryQualityRank[t.MinEntryQuality] {
		return domain.JevDirectionNone, nil, reasonf("%s: entry_quality=%q < min=%q", ReasonEntryQualityBelowThreshold, *d.EntryQuality, t.MinEntryQuality)
	}
	if *d.ContinuationProbability < t.MinContinuationProbability {
		return domain.JevDirectionNone, nil, reasonf("%s: continuation_probability=%.4f < min=%.4f", ReasonContinuationProbabilityBelowThreshold, *d.ContinuationProbability, t.MinContinuationProbability)
	}
	if *d.ToxicFlow > t.MaxToxicFlow {
		return domain.JevDirectionNone, nil, reasonf("%s: toxic_flow=%.4f > max=%.4f", ReasonToxicFlowAboveThreshold, *d.ToxicFlow, t.MaxToxicFlow)
	}
	if *d.LiquidityStressed > t.MaxLiquidityStressed {
		return domain.JevDirectionNone, nil, reasonf("%s: liquidity_stressed=%.4f > max=%.4f", ReasonLiquidityStressedAboveThreshold, *d.LiquidityStressed, t.MaxLiquidityStressed)
	}
	score := probability
	return direction, &score, nil
}

// reasonf formats a reject_reason string and returns it as *string
// (domain.TradeSignal.RejectReason's type). format may be a plain
// constant (no verbs).
func reasonf(format string, args ...any) *string {
	s := textutil.Truncate(fmt.Sprintf(format, args...), maxRejectReasonBytes)
	return &s
}

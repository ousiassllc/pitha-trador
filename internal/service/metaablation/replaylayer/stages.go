package replaylayer

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// replayStages are the stages the replay layer counts, in the live application
// order (policy.decideDirection, decideMeta, then the Risk Engine and the fill).
// The label layer's "labeled" and "features" stages are not part of a replay.
var replayStages = []metaablation.Stage{
	metaablation.StageDecision, metaablation.StageEligibility, metaablation.StageQuality,
	metaablation.StageFlowGuards, metaablation.StageMeta, metaablation.StageRisk, metaablation.StageFill,
}

// stageOf is the first stage that rejected an evaluated decision, and the
// reason's key; ok is false for a decision that opened a position. The stage is
// read from the reject reason the live decision tree produced, whose order is
// the application order, so a row failing several stages is counted at the first.
func stageOf(ev backtest.EntryEvaluation) (stage metaablation.Stage, reason string, ok bool) {
	switch ev.Outcome {
	case backtest.OutcomeOpened:
		return 0, "", false
	case backtest.OutcomeNoSize:
		return metaablation.StageRisk, "no_size", true
	case backtest.OutcomeNoFill:
		return metaablation.StageFill, "no_fill", true
	}
	if notACandidate(ev) {
		// Counted at the decision stage first, as the label layer's eligibility
		// package does, even when the row also fails an eligibility reason.
		return metaablation.StageDecision, ReasonNoCandidate, true
	}
	reason = ""
	if ev.Signal.RejectReason != nil {
		reason = *ev.Signal.RejectReason
	}
	return stageOfReason(reason), reasonKey(reason), true
}

// ReasonNoCandidate is the reason of a decision whose Jev direction is neither
// LONG nor SHORT (and that is no VWAP reversion candidate): the same string the
// label layer's eligibility stage uses.
const ReasonNoCandidate = "no_candidate"

// notACandidate reports a decision with no LONG/SHORT primary signal: Jev's
// direction is neither, and the VWAP reversion rule (which ignores Jev's
// direction) does not apply.
func notACandidate(ev backtest.EntryEvaluation) bool {
	d := ev.Decision
	return d.Direction != nil && !isReversion(ev) &&
		*d.Direction != domain.JevDirectionLong && *d.Direction != domain.JevDirectionShort
}

// stageOfReason maps a policy reject_reason to the stage that produced it.
func stageOfReason(reason string) metaablation.Stage {
	switch {
	case hasReason(reason, policy.ReasonJevNone):
		// Jev has no LONG/SHORT direction: not a candidate at all.
		return metaablation.StageDecision
	case hasReason(reason, policy.ReasonEntryQualityBelowThreshold):
		return metaablation.StageQuality
	case hasReason(reason, policy.ReasonContinuationProbabilityBelowThreshold),
		hasReason(reason, policy.ReasonToxicFlowAboveThreshold),
		hasReason(reason, policy.ReasonLiquidityStressedAboveThreshold):
		return metaablation.StageFlowGuards
	case hasReason(reason, policy.ReasonMetaSkip), hasReason(reason, policy.ReasonMetaModelUnavailable):
		return metaablation.StageMeta
	case hasReason(reason, policy.ReasonRiskEngineRejected):
		return metaablation.StageRisk
	default:
		// api_error, missing_data, not_calibrated, spread_too_wide, thin_liquidity,
		// special_quote, price_limit, not_lendable and the VWAP rule.
		return metaablation.StageEligibility
	}
}

func hasReason(reason, prefix string) bool {
	return reason == prefix || strings.HasPrefix(reason, prefix+":")
}

// reasonKey shortens a reject_reason to its name; meta and Risk rejections keep
// their second part (the verdict reason / the Risk rule), without numbers.
func reasonKey(reason string) string {
	parts := strings.SplitN(reason, ": ", 3)
	key := parts[0]
	if len(parts) > 1 && (key == policy.ReasonMetaSkip || key == policy.ReasonRiskEngineRejected || key == policy.ReasonMetaModelUnavailable) {
		second, _, _ := strings.Cut(parts[1], " ")
		second, _, _ = strings.Cut(second, ":")
		key += ":" + second
	}
	if key == "" {
		return "unknown"
	}
	return key
}

// stageCounts counts evaluated decisions by first rejecting stage.
func stageCounts(evals []backtest.EntryEvaluation, keep func(backtest.EntryEvaluation) bool) []StageCount {
	dropped := map[metaablation.Stage]int{}
	total := 0
	for _, ev := range evals {
		if !keep(ev) {
			continue
		}
		total++
		if stage, _, rejected := stageOf(ev); rejected {
			dropped[stage]++
		}
	}
	out := make([]StageCount, 0, len(replayStages))
	reached := total
	for _, s := range replayStages {
		out = append(out, StageCount{Stage: s.String(), Reached: reached, Dropped: dropped[s]})
		reached -= dropped[s]
	}
	return out
}

// reachedOf is the number of decisions that reached stage s.
func reachedOf(stages []StageCount, s metaablation.Stage) int {
	for _, c := range stages {
		if c.Stage == s.String() {
			return c.Reached
		}
	}
	return 0
}

// dropReasons counts the first rejecting reason per "stage:reason".
func dropReasons(evals []backtest.EntryEvaluation) map[string]int {
	out := map[string]int{}
	for _, ev := range evals {
		if stage, reason, rejected := stageOf(ev); rejected {
			out[stage.String()+":"+reason]++
		}
	}
	return out
}

// isReversion reports whether the evaluated decision is a VWAP reversion
// candidate (entry_quality and continuation never apply to it).
func isReversion(ev backtest.EntryEvaluation) bool {
	return ev.Decision.MarketRegime.UsesVWAPReversion()
}

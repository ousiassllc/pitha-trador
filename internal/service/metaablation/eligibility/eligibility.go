// Package eligibility reconstructs, for stored decisions, which stage of the
// live entry rule first rejects each one, separately for Q off and Q on
// (issue #1046).
//
// It does NOT read the first stored reject_reason: a row the entry_quality cut
// rejected first never had its later guards evaluated, so the stored reason
// cannot say whether Q off would have passed it. Instead every row is
// re-evaluated from its stored features and decision against the fixed rules
// with the live policy engine itself (policy.Engine.Decide: eligibility
// conditions -> chooseSide -> policy.ThresholdReject), so the order and the
// conditions are the engine's and cannot drift from a copy. The confidence
// threshold is never used (MinProbability 0, as in live's meta rule), the
// calibration gate is satisfied, and Risk is AlwaysPass: only the stages up to
// the M stage are evaluated here.
//
// A row that cannot be recomputed (market data missing, features
// incomplete) is "unknown" and excluded from all four cells identically, with
// the reason counted: the population is then limited to the recomputable rows.
package eligibility

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Reasons of an unknown row.
const (
	UnknownFeaturesIncomplete = "features_incomplete"
	UnknownUnmapped           = "unmapped_policy_reason"
)

// Input is one stored decision with the market inputs it had.
type Input struct {
	ID int64
	// Labeled is whether the decision has a usable stored label.
	Labeled bool
	// Unrecoverable is non-empty when the stored inputs cannot be rebuilt for
	// a reason the loader knows (e.g. "market_data_unavailable"): the row is
	// unknown.
	Unrecoverable string
	// Policy is the policy engine's input rebuilt from the stored decision and
	// snapshot (Decision, SpreadBps, Turnover5mJPY, SpecialQuote, PriceLimit,
	// Lendable, APIErr). Calibrated is ignored.
	Policy policy.Input
	// Features is the meta model's input.
	Features metalabel.Features
}

// Outcome is where a row first drops out for one Q setting: Stage is 0 when it
// passed every stage up to (not including) the M stage.
type Outcome struct {
	Stage  metaablation.Stage
	Reason string
}

// Result is the reconstruction of one row.
type Result struct {
	ID int64
	// Unknown is the reason the row cannot be recomputed ("" when it can).
	Unknown string
	// Reversion is set for a VWAP reversion candidate: its side is not Jev's,
	// Q does not apply to it and its label does not belong to its side, so it
	// is reported separately.
	Reversion bool
	// Side is the candidate side (the policy's chooseSide result) of a row
	// that passed every stage before M with Q off; the same row has the same
	// side with Q on whenever it passes there. "" when it dropped out.
	Side string
	// QOff and QOn are the first rejecting stage with the entry_quality cut
	// not applied / applied.
	QOff, QOn Outcome
}

// Outcome returns the outcome for a Q setting.
func (r Result) Outcome(qOn bool) Outcome {
	if qOn {
		return r.QOn
	}
	return r.QOff
}

// Eligible is whether the row is a candidate common to all four cells: it can
// be recomputed and passed the eligibility conditions (Q is a later stage, so
// it is the same with Q off and Q on).
func (r Result) Eligible() bool {
	return r.Unknown == "" && (r.QOff.Stage == 0 || r.QOff.Stage >= metaablation.StageQuality)
}

// Evaluator reconstructs rows under fixed rules.
type Evaluator struct {
	qOff, qOn    *policy.Engine
	requireLabel bool
}

// New builds an Evaluator from the pre-registered rules. requireLabel makes a
// row without a label drop at the labeled stage (the label layer).
func New(r manifest.Rules, requireLabel bool) (*Evaluator, error) {
	if r.QualityCut != metaablation.QualityCut {
		return nil, fmt.Errorf("eligibility: quality cut %q, fixed to %q", r.QualityCut, metaablation.QualityCut)
	}
	engine := func(minQuality string) *policy.Engine {
		side := func(g manifest.FlowGuards) config.PolicyDirectionThresholds {
			return config.PolicyDirectionThresholds{
				// The confidence threshold is never used: live's meta rule
				// replaces it with the model's verdict.
				MinProbability: 0, MinEntryQuality: minQuality,
				MinContinuationProbability: g.MinContinuationProbability, MaxToxicFlow: g.MaxToxicFlow, MaxLiquidityStressed: g.MaxLiquidityStressed,
			}
		}
		return policy.NewEngine(policy.Thresholds{
			Policy:       config.PolicyConfig{Long: side(r.Long), Short: side(r.Short)},
			MaxSpreadBps: r.Eligibility.MaxSpreadBps, MinTurnover5mJPY: r.Eligibility.MinTurnover5mJPY,
		}, nil, nil)
	}
	return &Evaluator{qOff: engine(domain.JevEntryQualityPoor), qOn: engine(metaablation.QualityCut), requireLabel: requireLabel}, nil
}

// Evaluate reconstructs one row. Stages are checked in the live order:
// decision, labeled, features (unknown), then the engine's eligibility,
// entry_quality and flow guards.
func (e *Evaluator) Evaluate(ctx context.Context, in Input) Result {
	res := Result{ID: in.ID}
	d := in.Policy.Decision
	if d != nil {
		res.Reversion = d.MarketRegime.UsesVWAPReversion()
	}
	switch {
	case in.Policy.APIErr == nil && d != nil && d.Direction != nil && !res.Reversion &&
		*d.Direction != domain.JevDirectionLong && *d.Direction != domain.JevDirectionShort:
		res.QOff, res.QOn = Outcome{metaablation.StageDecision, "no_candidate"}, Outcome{metaablation.StageDecision, "no_candidate"}
		return res
	case e.requireLabel && !in.Labeled:
		res.QOff, res.QOn = Outcome{metaablation.StageLabeled, "no_label"}, Outcome{metaablation.StageLabeled, "no_label"}
		return res
	case in.Unrecoverable != "":
		res.Unknown = in.Unrecoverable
		return res
	case len(in.Features.MissingNames()) > 0:
		res.Unknown = UnknownFeaturesIncomplete
		return res
	}
	pin := in.Policy
	pin.Calibrated = true
	var unmapped string
	if res.QOff, res.Side, unmapped = run(ctx, e.qOff, pin); unmapped != "" {
		return Result{ID: in.ID, Unknown: UnknownUnmapped + ":" + unmapped}
	}
	if res.QOn, _, unmapped = run(ctx, e.qOn, pin); unmapped != "" {
		return Result{ID: in.ID, Unknown: UnknownUnmapped + ":" + unmapped}
	}
	return res
}

// run is one engine pass: the first rejecting stage, or the side of a row that
// passed.
func run(ctx context.Context, eng *policy.Engine, in policy.Input) (o Outcome, side, unmapped string) {
	sig := eng.Decide(ctx, in)
	if sig.RejectReason == nil {
		return Outcome{}, sig.Direction, ""
	}
	reason := *sig.RejectReason
	if i := strings.IndexByte(reason, ':'); i >= 0 {
		reason = reason[:i]
	}
	stage, ok := stageOf(reason)
	if !ok {
		return Outcome{}, "", reason
	}
	return Outcome{Stage: stage, Reason: reason}, "", ""
}

// stageOf maps the policy's reject reason prefix to the stage it belongs to.
func stageOf(reason string) (metaablation.Stage, bool) {
	switch reason {
	case policy.ReasonJevNone:
		return metaablation.StageDecision, true
	case policy.ReasonAPIError, policy.ReasonMissingData, policy.ReasonSpreadTooWide, policy.ReasonThinLiquidity,
		policy.ReasonSpecialQuote, policy.ReasonPriceLimit, policy.ReasonNotLendable,
		policy.ReasonVWAPZUnavailable, policy.ReasonVWAPNotExtreme:
		return metaablation.StageEligibility, true
	case policy.ReasonEntryQualityBelowThreshold:
		return metaablation.StageQuality, true
	case policy.ReasonContinuationProbabilityBelowThreshold, policy.ReasonToxicFlowAboveThreshold, policy.ReasonLiquidityStressedAboveThreshold:
		return metaablation.StageFlowGuards, true
	}
	return 0, false
}

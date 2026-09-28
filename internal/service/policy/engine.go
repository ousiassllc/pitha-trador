package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// Version identifies this build's Policy Engine threshold-evaluation
// logic, persisted to trade_signals.policy_version
// (docs/architecture/er.md §trade_signals, functional.md FR-POLICY-4/5).
// Bump it whenever the LONG/SHORT/NONE decision logic itself changes
// materially (config/strategy.yaml's policy.* values are versioned
// independently by ordinary config/deploy history).
const Version = "policy-v1"

// FR-POLICY-3 reject_reason prefixes: every NONE trade_signals row's
// RejectReason starts with one of these, describing which of the spec's
// listed NONE conditions applied.
const (
	ReasonJevNone                               = "jev_none"
	ReasonProbabilityBelowThreshold             = "probability_below_threshold"
	ReasonEntryQualityBelowThreshold            = "entry_quality_below_threshold"
	ReasonContinuationProbabilityBelowThreshold = "continuation_probability_below_threshold"
	ReasonToxicFlowAboveThreshold               = "toxic_flow_above_threshold"
	ReasonLiquidityStressedAboveThreshold       = "liquidity_stressed_above_threshold"
	ReasonSpreadTooWide                         = "spread_too_wide"
	ReasonThinLiquidity                         = "thin_liquidity"
	ReasonMissingData                           = "missing_data"
	ReasonAPIError                              = "api_error"
	ReasonNotCalibrated                         = "not_calibrated"
	ReasonRiskEngineRejected                    = "risk_engine_rejected"
)

// entryQualityRank orders domain.JevEntryQuality* from worst (0) to best,
// so FR-POLICY-1/2's "entry_quality >= strong" can be compared
// numerically. An unrecognized value ranks as 0 (poor): the lowest,
// conservative default - never lets an unknown value silently clear a
// higher threshold.
var entryQualityRank = map[string]int{
	domain.JevEntryQualityPoor:        0,
	domain.JevEntryQualityFair:        1,
	domain.JevEntryQualityGood:        2,
	domain.JevEntryQualityStrong:      3,
	domain.JevEntryQualityExceptional: 4,
}

// Thresholds bundles every configurable value Engine.Decide checks
// against. MaxSpreadBps/MinTurnover5mJPY reuse Fast Screener's own
// max_spread_bps/min_turnover_5m_jpy (config/strategy.yaml has no
// separate policy.* key for these): FR-POLICY-3's "スプレッド過大"/"板が
// 薄い" re-apply the same numeric filter Fast Screener already enforces,
// at the later point where Jev Trader has responded and the market state
// may have moved since screening.
type Thresholds struct {
	Policy           config.PolicyConfig
	MaxSpreadBps     float64
	MinTurnover5mJPY float64
}

// ThresholdsFromStrategy builds Thresholds from a full StrategyConfig
// (config.LoadStrategy's result).
func ThresholdsFromStrategy(cfg config.StrategyConfig) Thresholds {
	return Thresholds{
		Policy:           cfg.Policy,
		MaxSpreadBps:     cfg.FastScreener.MaxSpreadBps,
		MinTurnover5mJPY: cfg.FastScreener.MinTurnover5mJPY,
	}
}

// RiskChecker reports whether Risk Engine (functional.md §4.7,
// internal/service/risk.Engine) allows an otherwise-passing LONG/SHORT
// candidate to become a tradeable signal (FR-POLICY-3 "Risk Engine拒否").
// This interface is the extension point a real implementation
// substitutes without changing Engine's own logic.
type RiskChecker interface {
	// Check returns (true, "") when direction (domain.JevDirectionLong or
	// domain.JevDirectionShort) is allowed for instrumentID, or
	// (false, reason) when Risk Engine rejects it.
	Check(ctx context.Context, instrumentID int64, direction string) (passed bool, reason string)
}

// AlwaysPassRiskChecker is the placeholder RiskChecker used as this
// package's zero-value default until a later sub-scope's cmd/ wiring
// constructs and injects a real internal/service/risk.Engine: every
// candidate passes ("risk_passed（本スコープではRisk Engine未接続のため
// 暫定値...）", issue #33).
type AlwaysPassRiskChecker struct{}

// Check always reports passed.
func (AlwaysPassRiskChecker) Check(context.Context, int64, string) (bool, string) {
	return true, ""
}

// Input is one Policy Engine evaluation's input: Jev Trader's output for
// one instrument's current state (functional.md §4.5), plus the
// additional signals FR-POLICY-3's NONE conditions need that are not Jev
// Trader fields themselves.
type Input struct {
	InstrumentID int64
	Symbol       string
	Timestamp    time.Time

	// Decision is Jev Trader's output for this instrument/timestamp, or
	// nil when the Jev Trader call itself failed (FR-POLICY-3 "API異常":
	// see APIErr) or produced no decision at all.
	Decision *domain.JevDecision

	// EntryPriceReference is copied verbatim to the persisted
	// trade_signals.entry_price_reference column
	// (docs/architecture/er.md §trade_signals) regardless of Direction.
	EntryPriceReference *float64

	// SpreadBps is the current bid/ask spread (basis points), re-checked
	// here since price action between Fast Screener's own spread filter
	// and this Jev Trader response can widen it (FR-POLICY-3 "スプレッド
	// 過大"). nil means board data was unavailable this cycle
	// (FR-POLICY-3 "データ欠損"), which - like Fast Screener's own
	// PassesFilter (internal/service/screener) - fails the candidate
	// rather than passing it through.
	SpreadBps *float64

	// Turnover5mJPY is the trailing 5-minute turnover, re-checked against
	// the same Fast Screener min_turnover_5m_jpy limit (FR-POLICY-3 "板
	// が薄い"). It is not yet computed anywhere in this codebase (Feature
	// Engine does not derive it - see internal/service/screener.Input's
	// own doc comment for the same gap), so real jev-trader queue jobs
	// leave it nil, which skips this check rather than failing the
	// candidate; a later sub-scope that wires a real value in makes the
	// check take effect. Unit tests set it directly to exercise the "板
	// が薄い" branch.
	Turnover5mJPY *float64

	// Calibrated is false for an instrument/setup Calibration
	// (functional.md §4.9, a separate later sub-scope) has not yet
	// covered (FR-POLICY-3 "キャリブレーション対象外"). Real jev-trader
	// queue jobs default this true until Calibration exists to say
	// otherwise.
	Calibrated bool

	// APIErr is set by the caller when the Jev Trader call itself failed
	// (FR-POLICY-3 "API異常"); Decision is nil in that case.
	APIErr error
}

// Engine evaluates Input against Thresholds/RiskChecker (FR-POLICY-1〜3)
// and persists the resulting domain.TradeSignal via signals
// (FR-POLICY-5).
type Engine struct {
	thresholds Thresholds
	risk       RiskChecker
	signals    *repository.SignalRepository
	policy     PolicySource // optional, see WithPolicySource
}

// NewEngine returns an Engine using thresholds and risk to decide, and
// signals to persist every decision. risk defaults to
// AlwaysPassRiskChecker when nil.
func NewEngine(thresholds Thresholds, risk RiskChecker, signals *repository.SignalRepository, opts ...Option) *Engine {
	if risk == nil {
		risk = AlwaysPassRiskChecker{}
	}
	e := &Engine{thresholds: thresholds, risk: risk, signals: signals}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Decide evaluates in against every FR-POLICY-1〜3 condition and returns
// the resulting domain.TradeSignal, unpersisted. Evaluate wraps Decide
// with persistence (FR-POLICY-5); Decide itself performs no I/O other
// than the injected RiskChecker.
//
// Every call emits a structured JSON log line for non-functional.md
// §5.1's "Signal count（生成シグナル数）" (counting "policy: trade signal
// decided" log lines); a Risk Engine rejection additionally emits its
// own "policy: risk engine rejected signal" line for §5.1's "Risk拒否
// 件数" (counting those log lines is a narrower count than every
// direction=none signal, since some become none for other FR-POLICY-1〜3
// reasons - missing data, calibration exclusion, spread too wide, ...).
func (e *Engine) Decide(ctx context.Context, in Input) domain.TradeSignal {
	return e.decide(ctx, in, e.thresholds)
}

func (e *Engine) decide(ctx context.Context, in Input, th Thresholds) domain.TradeSignal {
	sig := domain.TradeSignal{
		InstrumentID:        in.InstrumentID,
		Symbol:              in.Symbol,
		Timestamp:           in.Timestamp,
		Direction:           domain.JevDirectionNone,
		EntryPriceReference: in.EntryPriceReference,
		PolicyVersion:       Version,
	}
	if in.Decision != nil {
		id := in.Decision.ID
		sig.JevDecisionID = &id
	}

	direction, score, reason := e.decideDirection(in, th)

	if direction != domain.JevDirectionNone {
		passed, riskReason := e.risk.Check(ctx, in.InstrumentID, direction)
		if !passed {
			slog.Warn("policy: risk engine rejected signal",
				"instrument_id", in.InstrumentID, "symbol", in.Symbol, "direction", direction, "reason", riskReason)
			direction = domain.JevDirectionNone
			score = nil
			combined := ReasonRiskEngineRejected
			if riskReason != "" {
				combined = fmt.Sprintf("%s: %s", ReasonRiskEngineRejected, riskReason)
			}
			reason = &combined
		}
	}

	sig.Direction = direction
	sig.Score = score
	sig.RiskPassed = direction != domain.JevDirectionNone
	sig.RejectReason = reason

	rejectReason := ""
	if reason != nil {
		rejectReason = *reason
	}
	slog.Info("policy: trade signal decided",
		"instrument_id", in.InstrumentID, "symbol", in.Symbol, "direction", sig.Direction,
		"risk_passed", sig.RiskPassed, "reject_reason", rejectReason)

	return sig
}

// Evaluate decides in against the currently-active thresholds
// (currentThresholds) and persists the result via the Engine's
// SignalRepository (FR-POLICY-5).
func (e *Engine) Evaluate(ctx context.Context, in Input) (domain.TradeSignal, error) {
	th, err := e.currentThresholds(ctx)
	if err != nil {
		return domain.TradeSignal{}, err
	}
	sig := e.decide(ctx, in, th)
	saved, err := e.signals.Insert(ctx, sig)
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf("policy: persist trade signal for %q: %w", in.Symbol, err)
	}
	return saved, nil
}

// decideDirection implements FR-POLICY-1〜3's decision tree, in order:
// API失敗 -> データ欠損/未評価 -> キャリブレーション対象外 -> スプレッド/
// 流動性 -> Jevのdirection別しきい値評価. It does not consult
// RiskChecker; Decide applies that as the final gate.
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

	d := in.Decision
	if d.Direction == nil || d.EntryQuality == nil || d.Confidence == nil ||
		d.ContinuationProbability == nil || d.ToxicFlow == nil || d.LiquidityStressed == nil {
		return domain.JevDirectionNone, nil, reasonf(ReasonMissingData)
	}

	switch *d.Direction {
	case domain.JevDirectionLong:
		return e.evaluateThreshold(domain.JevDirectionLong, th.Policy.Long, d)
	case domain.JevDirectionShort:
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
	s := fmt.Sprintf(format, args...)
	return &s
}

package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// Version identifies this build's Policy Engine threshold-evaluation
// logic, persisted to trade_signals.policy_version
// (docs/architecture/er.md §trade_signals, functional.md FR-POLICY-4/5).
// Bump it whenever the LONG/SHORT/NONE decision logic itself changes
// materially. Evaluate (the live path) records it alone while the
// config/strategy.yaml baseline thresholds are in effect, and suffixed
// with the applied Self-Improvement proposal's policy_version
// ("policy-v1+sol-12") while a runtime_settings override applied by
// FR-SELFIMPROVE-5 is, returning to plain Version after a rollback
// (see PolicySource.AppliedPolicyVersion). Decide, the backtest replay
// path, always records plain Version.
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
	ReasonSpecialQuote                          = "special_quote"
	ReasonPriceLimit                            = "price_limit"
	ReasonNotLendable                           = "not_lendable"
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

	// Turnover5mJPY is the trailing 5-minute turnover (Feature.Turnover5m:
	// the difference of kabuステーションAPI's cumulative session turnover
	// over 5 minutes), re-checked against the same Fast Screener
	// min_turnover_5m_jpy limit (FR-POLICY-3 "板が薄い"). nil means it
	// could not be computed (insufficient history) and skips this check.
	Turnover5mJPY *float64

	// SpecialQuote and PriceLimit are the snapshot's entry-eligibility
	// flags: a 特別気配 or a stop-high/stop-low bar cannot be filled, so
	// either way FR-POLICY-3 returns NONE even if Fast Screener passed it
	// on an earlier bar (issue #511). Lendable is false when the
	// instrument is not 貸借銘柄, which blocks only a SHORT; nil (unknown)
	// blocks nothing. The zero values restrict nothing.
	SpecialQuote bool
	PriceLimit   domain.PriceLimit
	Lendable     *bool

	// Calibrated is false when Calibration (functional.md §4.9) does not
	// yet cover the decision - its confidence bucket has too few labeled
	// samples (FR-POLICY-3 "キャリブレーション対象外"). Handler derives it
	// via WithCalibration.
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
	signals    *trading.SignalRepository
	policy     PolicySource // optional, see WithPolicySource
}

// NewEngine returns an Engine using thresholds and risk to decide, and
// signals to persist every decision. risk defaults to
// AlwaysPassRiskChecker when nil.
func NewEngine(thresholds Thresholds, risk RiskChecker, signals *trading.SignalRepository, opts ...Option) *Engine {
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
	th, policyVersion, err := e.currentThresholds(ctx)
	if err != nil {
		return domain.TradeSignal{}, err
	}
	sig := e.decide(ctx, in, th)
	sig.PolicyVersion = policyVersion
	saved, err := e.signals.Insert(ctx, sig)
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf("policy: persist trade signal for %q: %w", in.Symbol, err)
	}
	return saved, nil
}

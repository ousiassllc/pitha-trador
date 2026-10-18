package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// MetaVersion identifies the live entry rule that replaced the confidence
// threshold with the meta-labeling model's expected-value verdict (issue #765,
// parent #752); Evaluate records it (with the applied proposal's suffix, like
// Version) in trade_signals.policy_version, and the backtest replay
// (DecideReplay, issue #775) runs the same rule. Version stays the legacy
// rule's identifier.
const MetaVersion = "policy-v2"

// Reasons of a NONE the meta-labeling entry rule produced (FR-POLICY-3's
// reject_reason prefixes).
const (
	// ReasonMetaSkip: the secondary model scored (or could not score) the
	// Jev candidate and decided to skip it; the metalabel reason follows
	// (no_active_model, missing_feature, ev_not_above_minimum, ...).
	ReasonMetaSkip = "meta_skip"
	// ReasonMetaModelUnavailable: the model or its settings could not be
	// loaded (or none is wired); the entry is skipped, never guessed.
	ReasonMetaModelUnavailable = "meta_model_unavailable"
)

// MetaModelSource supplies the active secondary model; *metalabel.Store
// implements it. It returns (nil, nil) when no model has passed the OOS bar:
// the entry is then skipped.
type MetaModelSource interface {
	ActiveModel(ctx context.Context) (*metalabel.Model, error)
}

// PredictionRecorder stores the model's verdict on one Jev decision
// (metalabel_predictions); *metalabel.Store implements it.
type PredictionRecorder interface {
	RecordPrediction(ctx context.Context, p repo.Prediction) (repo.Prediction, error)
}

// MetaConfigSource supplies the take/skip policy (the Settings screen's
// minimum EV) at every decision, so a change applies without a restart.
type MetaConfigSource func(ctx context.Context) (metalabel.DecideConfig, error)

// TDnetScorer is the TDnet disclosure score of a symbol
// (internal/service/newsfeed.Service implements it): the meta-labeling model's
// tdnet_score feature, 0 when the symbol has no recent disclosure.
type TDnetScorer interface {
	TDnetScore(symbol string, at time.Time) float64
}

// WithTDnetScorer makes Handler fill Input.TDnetScore from src. Without it the
// feature is missing and the meta-labeling model skips every entry.
func WithTDnetScorer(src TDnetScorer) HandlerOption {
	return func(h *Handler) { h.tdnet = src }
}

type metaWiring struct {
	models      MetaModelSource
	predictions PredictionRecorder
	config      MetaConfigSource
}

// WithMetaModel makes Evaluate - the live path - enter a LONG/SHORT candidate
// only when the meta-labeling model's calibrated expected value is above zero
// and the configured minimum, instead of Jev's confidence clearing
// policy.*.min_probability (issue #765). Every verdict is stored through
// predictions, one row per Jev decision. Without this option (or without a
// model) Evaluate never enters: a NONE whose reject_reason says why.
// DecideReplay is the same rule for the backtest, with the model that was in
// force at the replayed time.
func WithMetaModel(models MetaModelSource, predictions PredictionRecorder, config MetaConfigSource) Option {
	return func(e *Engine) { e.meta = &metaWiring{models: models, predictions: predictions, config: config} }
}

// metaEntry is what the meta-labeling entry rule needs for one decision,
// loaded before the pure decision tree runs. A nil *metaEntry selects the
// legacy threshold rule.
type metaEntry struct {
	model *metalabel.Model // nil: no active model
	cfg   metalabel.DecideConfig
	// unavailable is non-empty when the model or settings could not be
	// loaded (or none is wired): the entry is skipped with this detail.
	unavailable string
}

// entryDetail is what decide learned besides the signal: the direction before
// the Risk Engine's gate and the model's verdict (nil when the candidate never
// reached the model or the legacy rule ran).
type entryDetail struct {
	candidate string
	verdict   *metalabel.Decision
}

// loadMeta loads the model and the take/skip policy for one decision. It never
// fails: whatever cannot be loaded is reported in metaEntry.unavailable and
// skips the entry.
func (e *Engine) loadMeta(ctx context.Context) *metaEntry {
	if e.meta == nil {
		return &metaEntry{unavailable: "no meta model configured"}
	}
	cfg, err := e.meta.config(ctx)
	if err != nil {
		slog.Error("policy: load meta-label settings; skipping the entry", "error", err)
		return &metaEntry{unavailable: fmt.Sprintf("settings: %v", err)}
	}
	model, err := e.meta.models.ActiveModel(ctx)
	if err != nil {
		slog.Error("policy: load the active meta-label model; skipping the entry", "error", err)
		return &metaEntry{unavailable: fmt.Sprintf("model: %v", err)}
	}
	return &metaEntry{model: model, cfg: cfg}
}

// metaFeatures assembles the model's raw inputs from the evaluation's input:
// the snapshot's order-flow features, spread and 1-minute return, the Jev
// answer's score and the TDnet score. A missing one stays nil, which the model
// treats as a skip.
func metaFeatures(in Input) metalabel.Features {
	var jevScore *float64
	if in.Decision != nil {
		if score, ok := jev.ScoreOf(*in.Decision); ok {
			v := score.Value()
			jevScore = &v
		}
	}
	return metalabel.NewFeatures(in.OrderFlow, in.SpreadBps, in.Return1m, jevScore, in.TDnetScore)
}

// decideMeta is the meta-labeling entry rule for a screened candidate: the
// side is the primary signal (Jev's, or the VWAP reversion side when reversion
// is set), the model decides whether to take it. The
// entry_quality/continuation/toxic-flow/liquidity guards still apply (they are
// hard safety conditions, not a probability; a reversion side keeps only the
// toxic-flow and liquidity ones, see flowGuards); only the confidence
// threshold is replaced by the model's verdict.
func (e *Engine) decideMeta(side string, t config.PolicyDirectionThresholds, in Input, m *metaEntry, reversion bool) (string, *float64, *string, *metalabel.Decision) {
	if m.unavailable != "" {
		return domain.JevDirectionNone, nil, reasonf("%s: %s", ReasonMetaModelUnavailable, m.unavailable), nil
	}
	if reversion {
		if reason := flowGuards(t, in.Decision); reason != nil {
			return domain.JevDirectionNone, nil, reason, nil
		}
	} else if direction, score, reason := e.evaluateThreshold(side, t, in.Decision, false); reason != nil {
		return direction, score, reason, nil
	}
	verdict := metalabel.Decide(m.model, side, metaFeatures(in), m.cfg)
	if verdict.Action != repo.ActionTake {
		return domain.JevDirectionNone, nil, reasonf("%s: %s (p=%.4f ev_bps=%.2f)", ReasonMetaSkip, verdict.Reason, verdict.CalibratedProbability, verdict.ExpectedValueBps), &verdict
	}
	score := verdict.CalibratedProbability
	return side, &score, nil, &verdict
}

// recordPrediction stores the model's verdict for the decision in. Nothing is
// stored without a model (there is no model row to reference) or a persisted
// Jev decision. SizeFraction is stored for analysis only: the order quantity
// ignores it (issue #781, FR-META-3).
func (e *Engine) recordPrediction(ctx context.Context, in Input, m *metaEntry, verdict *metalabel.Decision) error {
	if verdict == nil || m == nil || m.model == nil || in.Decision == nil || in.Decision.ID == 0 || e.meta == nil {
		return nil
	}
	_, err := e.meta.predictions.RecordPrediction(ctx, repo.Prediction{
		ModelID: m.model.ID, JevDecisionID: in.Decision.ID, PredictedAt: e.now().UTC(),
		RawProbability: verdict.RawProbability, CalibratedProbability: verdict.CalibratedProbability,
		ExpectedValueBps: verdict.ExpectedValueBps, Action: verdict.Action, SizeFraction: verdict.SizeFraction,
		Reason: verdict.Reason, FeaturesJSON: verdict.FeaturesJSON,
	})
	if err != nil {
		return fmt.Errorf("policy: record the meta-label prediction for jev decision %d: %w", in.Decision.ID, err)
	}
	return nil
}

// logComparison writes the paper comparison of the entry rule that replaced
// the confidence threshold against the legacy rule on the same input: both
// directions are before the Risk Engine's gate, so only the entry rules are
// compared.
func (e *Engine) logComparison(in Input, th Thresholds, m *metaEntry, detail entryDetail) {
	legacy, _, legacyReason, _ := e.decideDirection(in, th, nil)
	attrs := []any{
		"instrument_id", in.InstrumentID, "symbol", in.Symbol,
		"legacy_direction", legacy, "legacy_reason", derefString(legacyReason),
		"meta_direction", detail.candidate, "agree", legacy == detail.candidate,
	}
	if in.Decision != nil {
		attrs = append(attrs, "jev_decision_id", in.Decision.ID)
		if in.Decision.Confidence != nil {
			attrs = append(attrs, "jev_confidence", *in.Decision.Confidence)
		}
	}
	if m != nil && m.model != nil {
		attrs = append(attrs, "model_version", m.model.Version)
	}
	if v := detail.verdict; v != nil {
		attrs = append(attrs, "meta_action", v.Action, "meta_reason", v.Reason,
			"calibrated_probability", v.CalibratedProbability, "expected_value_bps", v.ExpectedValueBps, "size_fraction", v.SizeFraction)
	}
	slog.Info("policy: meta-label vs legacy entry decision", attrs...)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

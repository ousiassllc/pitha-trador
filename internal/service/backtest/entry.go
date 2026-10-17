package backtest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// EntryMode selects the rule that decides whether a Jev candidate is entered.
type EntryMode string

const (
	// EntryMeta is the default and the live rule (FR-POLICY-6, issue #765): a
	// LONG/SHORT candidate that clears the hard guards is entered only when the
	// meta-labeling model's calibrated expected value is above zero and above
	// the minimum EV. The model is the one already trained and active at the
	// decision time (ModelSource), never a later one: no look-ahead.
	EntryMeta EntryMode = ""
	// EntryLegacyConfidence is the pre-#765 rule - Jev's confidence against
	// policy.*.min_probability - kept only as an explicitly named comparison
	// mode, to show how differently that rule would have evaluated the same
	// history. A replay never uses it unless RunConfig.Entry says so.
	EntryLegacyConfidence EntryMode = "legacy_confidence"
)

// ModelSource says which meta-labeling model was in force at a point in time
// (internal/service/backtest/metahistory.Timeline over the stored
// metalabel_models history).
type ModelSource interface {
	// ModelAt returns the newest model trained and active before at, or
	// (nil, nil) when none existed yet - the replay then skips the entry, as
	// live does without an active model. An error means the model in force
	// could not be used (live's meta_model_unavailable): the entry is skipped.
	ModelAt(at time.Time) (*metalabel.Model, error)
}

// decideEntry runs the entry rule on in (bar's Policy Engine input, Decision
// set when one is available) with st's engine: the Risk Engine gate, when the
// replay has one, is part of the engine.
func (st *instState) decideEntry(ctx context.Context, in policy.Input) domain.TradeSignal {
	if st.cfg.Entry == EntryLegacyConfidence {
		return st.engine.Decide(ctx, in)
	}
	var model *metalabel.Model
	var modelErr error
	if in.Decision != nil && st.cfg.Models != nil {
		model, modelErr = st.cfg.Models.ModelAt(in.Decision.Timestamp)
	}
	if in.Decision != nil {
		in.TDnetScore = tdnetScore(*in.Decision)
	}
	meta := st.cfg.Meta
	if meta == (metalabel.DecideConfig{}) {
		meta = metalabel.DefaultDecideConfig()
	}
	sig, _ := st.engine.DecideReplay(ctx, in, model, modelErr, meta)
	return sig
}

// tdnetScore is the TDnet disclosure score the model's feature carries: the
// score of the news_context recorded with the Jev decision at its decision
// time (the rule training uses to rebuild features); a decision with no state
// has no recent disclosure, a neutral 0, and an unreadable state is nil (a
// missing feature: the model skips rather than guess).
func tdnetScore(d domain.JevDecision) *float64 {
	score := newsfeed.TDnetScoreNeutral
	if d.StateJSON != "" {
		var state struct {
			NewsContext *domain.NewsContext `json:"news_context"`
		}
		if err := json.Unmarshal([]byte(d.StateJSON), &state); err != nil {
			return nil
		}
		if state.NewsContext != nil {
			score = newsfeed.ScoreNewsContext(state.NewsContext.Items, d.Timestamp, newsfeed.DefaultTTL)
		}
	}
	return &score
}

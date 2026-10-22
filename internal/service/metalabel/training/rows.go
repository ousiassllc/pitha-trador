package training

import (
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// decisionState is the part of a persisted Jev input (jev.ScoutState, the
// jev_decisions.state_json) the model features come from.
type decisionState struct {
	Return1m    *float64            `json:"return_1m"`
	SpreadBps   *float64            `json:"spread_bps"`
	NewsContext *domain.NewsContext `json:"news_context"`
}

// BuildRows turns stored labeled decisions into walk-forward rows, rebuilding
// the features each decision had when it was made: the order-flow columns, the
// spread and 1-minute return of its state, the Jev score of its answer and the
// TDnet score of the news_context it was given (newsfeed.ScoreNewsContext at
// the decision time, the rule the live path uses; no news_context means no
// recent disclosure, a neutral 0). A decision whose features cannot be
// completed (a missing feature, an unreadable state or answer) is left out and
// counted in dropped: a model never learns from a made-up value.
func BuildRows(trs []repo.TrainingRow) (rows []Row, dropped int) {
	rows, dropped, _ = BuildRowsReport(trs)
	return rows, dropped
}

// BuildRowsReport is BuildRows that also counts, per feature name (the
// metalabel.FeatureNames), the decisions that lack the feature: one decision
// can lack several (an unreadable state leaves spread_bps and return_1m
// missing, an unreadable or unscorable answer leaves jev_score missing), so
// the counts overlap and add up to more than dropped. It lets a read-only
// caller (the Paper results MCP's metalabel_training_readiness) explain
// dropped_incomplete with the trainer's own rule, decoding each decision once.
func BuildRowsReport(trs []repo.TrainingRow) (rows []Row, dropped int, missing map[string]int) {
	missing = map[string]int{}
	for _, tr := range trs {
		features := buildFeatures(tr)
		for _, name := range features.MissingNames() {
			missing[name]++
		}
		row, ok := rowFrom(tr, features)
		if !ok {
			dropped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, dropped, missing
}

// rowFrom is the walk-forward row of tr with its rebuilt features, or false
// when the model input cannot be formed.
func rowFrom(tr repo.TrainingRow, features metalabel.Features) (Row, bool) {
	vector, err := features.Vector(tr.Direction)
	if err != nil {
		return Row{}, false
	}
	return Row{
		JevDecisionID: tr.JevDecisionID, DecidedAt: tr.DecidedAt, LabelEnd: tr.LabelEnd(),
		JevModelID: tr.JevModelID, Symbol: tr.Symbol, Direction: tr.Direction, Features: features, Vector: vector, NetReturnBps: tr.NetReturnBps,
	}, true
}

// buildFeatures rebuilds the features of tr; a feature that cannot be rebuilt
// stays nil (never a made-up value).
func buildFeatures(tr repo.TrainingRow) metalabel.Features {
	var state decisionState
	if err := json.Unmarshal([]byte(tr.StateJSON), &state); err != nil {
		state = decisionState{}
	}
	var jevScore *float64
	var answer jev.TraderResponse
	if err := json.Unmarshal([]byte(tr.ResponseJSON), &answer); err == nil {
		if score, err := answer.Score(); err == nil {
			v := score.Value()
			jevScore = &v
		}
	}
	tdnet := newsfeed.TDnetScoreNeutral
	if state.NewsContext != nil {
		tdnet = newsfeed.ScoreNewsContext(state.NewsContext.Items, tr.DecidedAt, newsfeed.DefaultTTL)
	}
	return metalabel.NewFeatures(tr.OrderFlow, state.SpreadBps, state.Return1m, jevScore, &tdnet)
}

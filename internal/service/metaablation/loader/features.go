package loader

import (
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// decisionState is the part of a stored Jev input (jev_decisions.state_json)
// the model features come from.
type decisionState struct {
	Return1m    *float64            `json:"return_1m"`
	SpreadBps   *float64            `json:"spread_bps"`
	NewsContext *domain.NewsContext `json:"news_context"`
}

// BuildFeatures rebuilds the model input a stored decision had when it was
// made, by the rule the training uses (training.BuildRows, which this package
// must not import: it holds the trainer): the order-flow columns, the spread and
// 1-minute return of the state, the Jev score of the answer and the TDnet
// score of the state's news_context at the decision time (no news_context is
// the neutral 0). A feature that cannot be rebuilt stays nil, never a made-up
// value. A test pins this function to training.BuildRows.
func BuildFeatures(d domain.JevDecision) metalabel.Features {
	var state decisionState
	if err := json.Unmarshal([]byte(d.StateJSON), &state); err != nil {
		state = decisionState{}
	}
	var jevScore *float64
	var answer jev.TraderResponse
	if err := json.Unmarshal([]byte(d.ResponseJSON), &answer); err == nil {
		if score, err := answer.Score(); err == nil {
			v := score.Value()
			jevScore = &v
		}
	}
	tdnet := newsfeed.TDnetScoreNeutral
	if state.NewsContext != nil {
		tdnet = newsfeed.ScoreNewsContext(state.NewsContext.Items, d.Timestamp, newsfeed.DefaultTTL)
	}
	return metalabel.NewFeatures(d.OrderFlow, state.SpreadBps, state.Return1m, jevScore, &tdnet)
}

// Cohort is the cohort / Jev version of a stored decision (jev.TraderCohort of
// its question version); "" when the version is unknown, which matches no
// manifest cohort and so excludes the row.
func Cohort(d domain.JevDecision) string {
	c, _ := jev.TraderCohort(d.QuestionVersion)
	return c
}

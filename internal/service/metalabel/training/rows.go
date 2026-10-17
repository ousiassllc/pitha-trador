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
	for _, tr := range trs {
		row, ok := buildRow(tr)
		if !ok {
			dropped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, dropped
}

func buildRow(tr repo.TrainingRow) (Row, bool) {
	var state decisionState
	if err := json.Unmarshal([]byte(tr.StateJSON), &state); err != nil {
		return Row{}, false
	}
	var answer jev.TraderResponse
	if err := json.Unmarshal([]byte(tr.ResponseJSON), &answer); err != nil {
		return Row{}, false
	}
	score, err := answer.Score()
	if err != nil {
		return Row{}, false
	}
	jevScore := score.Value()
	tdnet := newsfeed.TDnetScoreNeutral
	if state.NewsContext != nil {
		tdnet = newsfeed.ScoreNewsContext(state.NewsContext.Items, tr.DecidedAt, newsfeed.DefaultTTL)
	}
	features := metalabel.NewFeatures(tr.OrderFlow, state.SpreadBps, state.Return1m, &jevScore, &tdnet)
	vector, err := features.Vector(tr.Direction)
	if err != nil {
		return Row{}, false
	}
	return Row{
		JevDecisionID: tr.JevDecisionID, DecidedAt: tr.DecidedAt, LabelEnd: tr.LabelEnd(),
		JevModelID: tr.JevModelID, Symbol: tr.Symbol, Direction: tr.Direction, Features: features, Vector: vector, NetReturnBps: tr.NetReturnBps,
	}, true
}

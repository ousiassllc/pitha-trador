package metalabeldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// MaxJevModels is the most distinct jev_decisions.model_id values jev_models
// returns. The column holds whatever model name Jev answered with, so a period
// with more distinct names than this is limit_exceeded rather than cut.
const MaxJevModels = 50

// JevModelCount is one model_id's decisions in the period (issue #849).
// FirstSeen and LastSeen are the earliest and latest decision timestamps, in
// JST (RFC3339 with +09:00).
type JevModelCount struct {
	ModelID         string `json:"model_id"`
	Decisions       int64  `json:"decisions"`
	TraderDecisions int64  `json:"trader_decisions"`
	FirstSeen       string `json:"first_seen"`
	LastSeen        string `json:"last_seen"`
}

// JevModelDay is one JST day's decisions of one model_id.
type JevModelDay struct {
	Date      string `json:"date"`
	ModelID   string `json:"model_id"`
	Decisions int64  `json:"decisions"`
}

// JevModelCutoff compares metalabel.llm_cutoff_model with the model_id of the
// newest decision (any period, any decision type). CutoffModelMatch is one of
// training.CutoffModelMatch / CutoffModelMismatch / CutoffModelUnset, decided
// by training.CutoffModelStatus (the retraining's own exact comparison).
// LatestModelID and LatestAt are null while jev_decisions is empty.
type JevModelCutoff struct {
	LLMCutoffModel string  `json:"llm_cutoff_model"`
	LatestModelID  *string `json:"latest_model_id"`
	LatestAt       *string `json:"latest_at"`
	Match          string  `json:"match"`
}

// JevModels is jev_models' data.
type JevModels struct {
	Models []JevModelCount `json:"models"`
	Daily  []JevModelDay   `json:"daily"`
	Cutoff JevModelCutoff  `json:"cutoff"`
}

// inJST renders a stored timestamp as RFC3339 in JST; an unparseable value is
// returned unchanged (like FormatOut).
func inJST(stored string) string {
	t, err := sqlutil.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.In(query.JST).Format(time.RFC3339)
}

// JevModelsUsage reads the decisions of [r.From, r.To) per model_id (one
// statement grouped by JST day and model_id), and the model_id of the newest
// decision against the stored llm_cutoff_model. The decision rows' state_json,
// response_json and the secrets are not touched.
func JevModelsUsage(ctx context.Context, q query.Q, r query.Range) (JevModels, error) {
	from, to := r.Bounds()
	rows, err := q.QueryContext(ctx, `
SELECT substr(datetime(timestamp, '+9 hours'), 1, 10), model_id, COUNT(*),
	COALESCE(SUM(decision_type = 'trader'), 0), MIN(timestamp), MAX(timestamp)
FROM jev_decisions
WHERE timestamp >= ? AND timestamp < ?
GROUP BY 1, 2
ORDER BY 1, 2`, from, to)
	if err != nil {
		return JevModels{}, fmt.Errorf("query: jev models: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := JevModels{Models: []JevModelCount{}, Daily: []JevModelDay{}}
	index := map[string]int{}
	first, last := map[string]string{}, map[string]string{}
	for rows.Next() {
		var (
			d            JevModelDay
			trader       int64
			minTS, maxTS string
		)
		if err := rows.Scan(&d.Date, &d.ModelID, &d.Decisions, &trader, &minTS, &maxTS); err != nil {
			return JevModels{}, fmt.Errorf("query: scan jev models: %w", err)
		}
		out.Daily = append(out.Daily, d)
		i, ok := index[d.ModelID]
		if !ok {
			if len(index) >= MaxJevModels {
				return JevModels{}, query.ErrLimitExceeded
			}
			i = len(out.Models)
			index[d.ModelID] = i
			out.Models = append(out.Models, JevModelCount{ModelID: d.ModelID})
			first[d.ModelID], last[d.ModelID] = minTS, maxTS
		}
		m := &out.Models[i]
		m.Decisions += d.Decisions
		m.TraderDecisions += trader
		if minTS < first[d.ModelID] {
			first[d.ModelID] = minTS
		}
		if maxTS > last[d.ModelID] {
			last[d.ModelID] = maxTS
		}
	}
	if err := rows.Err(); err != nil {
		return JevModels{}, fmt.Errorf("query: jev models: %w", err)
	}
	_ = rows.Close()
	for i := range out.Models {
		m := &out.Models[i]
		m.FirstSeen, m.LastSeen = inJST(first[m.ModelID]), inJST(last[m.ModelID])
	}
	sort.SliceStable(out.Models, func(i, j int) bool { return out.Models[i].FirstSeen < out.Models[j].FirstSeen })

	if out.Cutoff, err = jevModelCutoff(ctx, q); err != nil {
		return JevModels{}, err
	}
	return out, nil
}

// jevModelCutoff is the cutoff comparison against the newest decision.
func jevModelCutoff(ctx context.Context, q query.Q) (JevModelCutoff, error) {
	settings, err := loadMetalabelSettings(ctx, q)
	if err != nil {
		return JevModelCutoff{}, err
	}
	c := JevModelCutoff{LLMCutoffModel: settings.LLMCutoffModel}
	var model, at string
	err = q.QueryRowContext(ctx, `
SELECT model_id, timestamp FROM jev_decisions ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&model, &at)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return JevModelCutoff{}, fmt.Errorf("query: latest jev model: %w", err)
	default:
		latestAt := inJST(at)
		c.LatestModelID, c.LatestAt = &model, &latestAt
	}
	latest := ""
	if c.LatestModelID != nil {
		latest = *c.LatestModelID
	}
	c.Match = training.CutoffModelStatus(settings.LLMCutoffModel, latest)
	return c, nil
}

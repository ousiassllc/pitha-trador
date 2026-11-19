package ledgerdiag

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// maxSourceRows bounds one candidate query; beyond it the call is query.ErrLimitExceeded.
const maxSourceRows = MaxPredictionRows

// liveScope is the SQL condition and arguments selecting the decisions of the live reference.
func liveScope(from, to, version, modelID string) (string, []any) {
	cond := ` d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ? AND d.question_version = ?`
	args := []any{from, to, version}
	if modelID != "" {
		cond += " AND d.model_id = ?"
		args = append(args, modelID)
	}
	return cond, args
}

// loadShadow reads jev_shadow_decisions (direction, model and cost only; never the raw response body) of the
// reference's decisions.
func (set *sourceSet) loadShadow(ctx context.Context, q query.Q, from, to, version, modelID string) error {
	cond, args := liveScope(from, to, version, modelID)
	rows, err := q.QueryContext(ctx, `
SELECT s.jev_decision_id, s.prompt_version, s.model_id, s.direction, s.request_cost, s.cost_is_estimate
FROM jev_shadow_decisions s JOIN jev_decisions d ON d.id = s.jev_decision_id
WHERE`+cond+` ORDER BY s.prompt_version, s.jev_decision_id LIMIT ?`, append(args, maxSourceRows+1)...)
	if err != nil {
		return fmt.Errorf("query: list shadow answers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var (
			id                        int64
			promptVersion, model, dir string
			cost                      float64
			estimate                  bool
		)
		if err := rows.Scan(&id, &promptVersion, &model, &dir, &cost, &estimate); err != nil {
			return fmt.Errorf("query: scan shadow answer: %w", err)
		}
		if n++; n > maxSourceRows {
			return query.ErrLimitExceeded
		}
		if _, ok := set.rowByID[id]; !ok {
			continue
		}
		s := set.get(KindShadow+":"+promptVersion, KindShadow, promptVersion)
		s.seen = true
		s.addModel(model)
		a := answer{dir: dir, usd: cost, hasUSD: true, cost: CostReported}
		if estimate {
			a.cost = CostUsageEstimate
		}
		s.answers[id] = a
	}
	return rows.Err()
}

// loadTrial reads jev_trial_decisions (status, direction, served model and cost only; never
// the raw response body or the error text) of the reference's decisions. Only status ok answers: a failed,
// refused or partial row is missing.
func (set *sourceSet) loadTrial(ctx context.Context, q query.Q, from, to, version, modelID string) error {
	cond, args := liveScope(from, to, version, modelID)
	rows, err := q.QueryContext(ctx, `
SELECT t.jev_decision_id, t.arm_id, t.status, t.direction, t.served_model, t.cost_source, t.counted_cost_usd
FROM jev_trial_decisions t JOIN jev_decisions d ON d.id = t.jev_decision_id
WHERE`+cond+` ORDER BY t.arm_id, t.jev_decision_id LIMIT ?`, append(args, maxSourceRows+1)...)
	if err != nil {
		return fmt.Errorf("query: list trial answers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var (
			id                          int64
			arm, status, source, served string
			dir                         sql.NullString
			counted                     float64
		)
		if err := rows.Scan(&id, &arm, &status, &dir, &served, &source, &counted); err != nil {
			return fmt.Errorf("query: scan trial answer: %w", err)
		}
		if n++; n > maxSourceRows {
			return query.ErrLimitExceeded
		}
		if _, ok := set.rowByID[id]; !ok {
			continue
		}
		s := set.get(KindTrial+":"+arm, KindTrial, arm)
		s.seen = true
		s.addModel(served)
		if status != "ok" || !dir.Valid {
			s.failed[id] = true
			continue
		}
		// counted_cost_usd is what the daily cap counted: for cost_source=unknown it is the worst-side
		// estimate, which is not a measured cost; a 0 there means "no amount", never a free call.
		a := answer{dir: dir.String, usd: counted, hasUSD: true, cost: source}
		if source == CostUnknown && counted == 0 {
			a.hasUSD = false
		}
		s.answers[id] = a
	}
	return rows.Err()
}

// loadResearchModel reads research_model_predictions of the period's JST days with their run's
// algorithm and label horizon. A decision predicted by several runs of one algorithm/horizon takes
// the latest run's direction.
func (set *sourceSet) loadResearchModel(ctx context.Context, q query.Q, r query.Range) error {
	fromDay := r.From.In(query.JST).Format(time.DateOnly)
	toDay := r.To.Add(-time.Nanosecond).In(query.JST).Format(time.DateOnly)
	rows, err := q.QueryContext(ctx, `
SELECT p.jev_decision_id, ru.algorithm, ru.label_horizon, p.direction
FROM research_model_predictions p JOIN research_model_runs ru ON ru.id = p.run_id
WHERE p.day >= ? AND p.day <= ? ORDER BY ru.id, p.jev_decision_id LIMIT ?`, fromDay, toDay, maxSourceRows+1)
	if err != nil {
		return fmt.Errorf("query: list research model predictions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var (
			id                      int64
			algorithm, horizon, dir string
		)
		if err := rows.Scan(&id, &algorithm, &horizon, &dir); err != nil {
			return fmt.Errorf("query: scan research model prediction: %w", err)
		}
		if n++; n > maxSourceRows {
			return query.ErrLimitExceeded
		}
		if _, ok := set.rowByID[id]; !ok {
			continue
		}
		key := algorithm + ":" + horizon
		s := set.get(KindResearchModel+":"+key, KindResearchModel, key)
		s.seen = true
		s.addModel(key) // the run version changes with every walk-forward retraining; the algorithm and horizon identify the model
		s.answers[id] = answer{dir: dir, hasUSD: true, cost: CostNone}
	}
	return rows.Err()
}

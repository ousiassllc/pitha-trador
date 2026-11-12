package trialquery

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// armRow is one jev_trial_decisions row as the comparison reads it: no body, no
// error text, no cost.
type armRow struct {
	status, failureCode, direction, entryQuality, cutoff string
	truncation, rag                                      bool
}

// decision is one live trader decision and the rows the selected arms gave it
// (rows[i] is arm i's, nil when the arm was not asked).
type decision struct {
	id      int64
	liveDir string
	rows    []*armRow
}

// ValidateArms checks the arm ids of a call: distinct, well-formed ids, at most MaxArms.
func ValidateArms(arms []string) error {
	if len(arms) > MaxArms {
		return fmt.Errorf("trialquery: at most %d arms", MaxArms)
	}
	seen := map[string]bool{}
	for _, a := range arms {
		if !armIDPattern.MatchString(a) || seen[a] {
			return fmt.Errorf("trialquery: invalid or repeated arm id")
		}
		seen[a] = true
	}
	return nil
}

// placeholders is "?, ?, ..." for n values.
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

type flatRow struct {
	decisionID int64
	liveDir    string
	arm        string
	row        armRow
}

// loadRows reads the trial rows of the live trader decisions of the period
// (timestamp in [from, to)), of the given arms or of every arm.
func loadRows(ctx context.Context, q query.Q, from, to string, arms []string) ([]flatRow, error) {
	args := []any{from, to}
	filter := ""
	if len(arms) > 0 {
		filter = ` AND t.arm_id IN (` + placeholders(len(arms)) + `)`
		for _, a := range arms {
			args = append(args, a)
		}
	}
	args = append(args, MaxRows+1)
	rows, err := q.QueryContext(ctx, `
SELECT t.jev_decision_id, COALESCE(d.direction, ''), t.arm_id, t.status, t.failure_code, COALESCE(t.direction, ''),
	COALESCE(t.entry_quality, ''), t.truncation_suspected, t.rag_includes_live_model_answers, t.llm_cutoff_status
FROM jev_trial_decisions t JOIN jev_decisions d ON d.id = t.jev_decision_id
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?`+filter+`
ORDER BY t.jev_decision_id, t.arm_id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query: jev trial rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []flatRow
	for rows.Next() {
		var f flatRow
		if err := rows.Scan(&f.decisionID, &f.liveDir, &f.arm, &f.row.status, &f.row.failureCode, &f.row.direction,
			&f.row.entryQuality, &f.row.truncation, &f.row.rag, &f.row.cutoff); err != nil {
			return nil, fmt.Errorf("query: scan jev trial rows: %w", err)
		}
		if len(out) == MaxRows {
			return nil, query.ErrLimitExceeded
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: jev trial rows: %w", err)
	}
	return out, nil
}

// groupDecisions turns the flat rows into decisions with one slot per arm.
// arms is the selected arm ids (empty: every arm present, sorted); the result
// is the arm list used and the decisions in id order.
func groupDecisions(flat []flatRow, arms []string) ([]string, []*decision, error) {
	if len(arms) == 0 {
		for _, f := range flat {
			if !slices.Contains(arms, f.arm) {
				arms = append(arms, f.arm)
			}
		}
		slices.Sort(arms)
		if len(arms) > MaxArms {
			return nil, nil, query.ErrLimitExceeded
		}
	}
	var decisions []*decision
	byID := map[int64]*decision{}
	for i := range flat {
		f := &flat[i]
		d := byID[f.decisionID]
		if d == nil {
			d = &decision{id: f.decisionID, liveDir: f.liveDir, rows: make([]*armRow, len(arms))}
			byID[f.decisionID] = d
			decisions = append(decisions, d)
		}
		if k := slices.Index(arms, f.arm); k >= 0 {
			d.rows[k] = &f.row
		}
	}
	return arms, decisions, nil
}

// outcomeKey names one (horizon, label version) slice of the outcome records.
type outcomeKey struct{ horizon, labelVersion int }

// loadOutcomes reads the existing calibration_outcomes records of the live
// decisions of the period that have a trial row: future_return per slice and
// decision. A record whose future_return is NULL carries no return and is skipped.
func loadOutcomes(ctx context.Context, q query.Q, from, to string) (map[outcomeKey]map[int64]float64, error) {
	rows, err := q.QueryContext(ctx, `
SELECT co.jev_decision_id, co.horizon_minutes, co.label_version, co.future_return
FROM calibration_outcomes co JOIN jev_decisions d ON d.id = co.jev_decision_id
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?
	AND EXISTS (SELECT 1 FROM jev_trial_decisions t WHERE t.jev_decision_id = d.id)
LIMIT ?`, from, to, MaxOutcomeRows+1)
	if err != nil {
		return nil, fmt.Errorf("query: jev trial outcomes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[outcomeKey]map[int64]float64{}
	n := 0
	for rows.Next() {
		var (
			id  int64
			k   outcomeKey
			ret sql.NullFloat64
		)
		if err := rows.Scan(&id, &k.horizon, &k.labelVersion, &ret); err != nil {
			return nil, fmt.Errorf("query: scan jev trial outcomes: %w", err)
		}
		if n++; n > MaxOutcomeRows {
			return nil, query.ErrLimitExceeded
		}
		if !ret.Valid {
			continue
		}
		if out[k] == nil {
			if len(out) == MaxSlices {
				return nil, query.ErrLimitExceeded
			}
			out[k] = map[int64]float64{}
		}
		out[k][id] = ret.Float64
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: jev trial outcomes: %w", err)
	}
	return out, nil
}

// cfLabel is one counterfactual_labels record: what the live direction would
// have done had the skipped candidate been entered.
type cfLabel struct {
	barrier string
	netBps  float64
}

// loadCounterfactual reads the existing counterfactual_labels of the live
// decisions of the period that have a trial row, per horizon and decision.
func loadCounterfactual(ctx context.Context, q query.Q, from, to string) (map[int]map[int64]cfLabel, error) {
	rows, err := q.QueryContext(ctx, `
SELECT c.jev_decision_id, c.horizon_minutes, c.barrier_hit, c.net_return_bps
FROM counterfactual_labels c JOIN jev_decisions d ON d.id = c.jev_decision_id
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?
	AND EXISTS (SELECT 1 FROM jev_trial_decisions t WHERE t.jev_decision_id = d.id)
LIMIT ?`, from, to, MaxOutcomeRows+1)
	if err != nil {
		return nil, fmt.Errorf("query: jev trial counterfactual labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]map[int64]cfLabel{}
	n := 0
	for rows.Next() {
		var (
			id      int64
			horizon int
			l       cfLabel
		)
		if err := rows.Scan(&id, &horizon, &l.barrier, &l.netBps); err != nil {
			return nil, fmt.Errorf("query: scan jev trial counterfactual labels: %w", err)
		}
		if n++; n > MaxOutcomeRows {
			return nil, query.ErrLimitExceeded
		}
		if out[horizon] == nil {
			if len(out) == MaxSlices {
				return nil, query.ErrLimitExceeded
			}
			out[horizon] = map[int64]cfLabel{}
		}
		out[horizon][id] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: jev trial counterfactual labels: %w", err)
	}
	return out, nil
}

package entryqualitydiag

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// versionScope is the SQL condition on jev_decisions.question_version of a.
func versionScope(a Args) (string, []any) {
	if a.PromptVersion != "" {
		return "d.question_version = ?", []any{a.PromptVersion}
	}
	return sqlutil.NotIn("d.question_version", a.Foreign)
}

func countDecisions(ctx context.Context, q query.Q, from, to, scope string, scopeArgs []any) (traders, longShort int, err error) {
	if scope != "" {
		scope = " AND " + scope
	}
	err = q.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(d.direction IN ('LONG', 'SHORT')), 0)
FROM jev_decisions d
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?`+scope,
		append([]any{from, to}, scopeArgs...)...).Scan(&traders, &longShort)
	if err != nil {
		return 0, 0, fmt.Errorf("query: count trader decisions: %w", err)
	}
	return traders, longShort, nil
}

// answerExpr is the JSON field of jev_decisions.response_json as a value of one
// of the given JSON types, NULL when the response is malformed, lacks the field
// or holds another type (the engine's missing_data, never an error).
func answerExpr(field string, types string) string {
	return `CASE WHEN json_valid(d.response_json) THEN CASE WHEN json_type(d.response_json, '$.` + field + `') IN (` + types +
		`) THEN json_extract(d.response_json, '$.` + field + `') END END`
}

func listDecisions(ctx context.Context, q query.Q, from, to, scope string, scopeArgs []any) ([]decisionRow, error) {
	if scope != "" {
		scope = " AND " + scope
	}
	const number = `'real', 'integer'`
	rows, err := q.QueryContext(ctx, `
SELECT d.id, d.question_version, d.direction, d.confidence, `+answerExpr("entry_quality", `'text'`)+`,
	`+answerExpr("continuation_probability", number)+`, `+answerExpr("toxic_flow", number)+`, `+answerExpr("liquidity_stressed", number)+`,
	d.ab_arm, d.market_regime
FROM jev_decisions d
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?`+scope+`
ORDER BY d.id`, append([]any{from, to}, scopeArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("query: list trader decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []decisionRow
	for rows.Next() {
		var r decisionRow
		if err := rows.Scan(&r.id, &r.version, &r.direction, &r.confidence, &r.entryQuality, &r.continuation, &r.toxicFlow, &r.liquidityStr, &r.abArm, &r.regime); err != nil {
			return nil, fmt.Errorf("query: scan trader decision: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: list trader decisions: %w", err)
	}
	return out, nil
}

// signalHeads maps each Jev decision of [from, to) to the reject_reason code
// of its newest trade signal ("" for a signal without one, i.e. one that
// passed). A decision's signal carries the decision's timestamp.
func signalHeads(ctx context.Context, q query.Q, from, to string) (map[int64]string, error) {
	rows, err := q.QueryContext(ctx, `
SELECT jev_decision_id, reject_reason FROM trade_signals
WHERE jev_decision_id IS NOT NULL AND timestamp >= ? AND timestamp < ?
ORDER BY id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query: list trade signals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var (
			id     int64
			reason sql.NullString
		)
		if err := rows.Scan(&id, &reason); err != nil {
			return nil, fmt.Errorf("query: scan trade signal: %w", err)
		}
		var r *string
		if reason.Valid {
			r = &reason.String
		}
		out[id] = head(r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: list trade signals: %w", err)
	}
	return out, nil
}

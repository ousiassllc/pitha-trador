package datasetexport

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Origins of a decision (the same rule as the calibration_outcome_origins view).
const (
	originEntered     = "entered"
	originNoSignal    = "no_signal"
	originNoDirection = "no_direction"
	originRiskReject  = "risk_rejected"
	originGuardSkip   = "guard_skipped"

	riskRejectedHead = "risk_engine_rejected"
)

// gate is what a decision's trade signals say.
type gate struct {
	count    int
	passed   bool
	lastID   int64
	lastPass bool
	lastWhy  string // the latest signal's reject_reason: read here, only its fixed code is ever written
}

// rejectCode is the fixed code of a reject_reason (query.RejectCode's, which maps an unknown
// reason to "other" and never carries numbers or error text), with the Risk Engine's own head.
func rejectCode(reason string) string {
	head, rest, _ := strings.Cut(reason, ":")
	head, rest = strings.TrimSpace(head), strings.TrimSpace(rest)
	if head == riskRejectedHead {
		return riskRejectedHead
	}
	sub, _, _ := strings.Cut(rest, " ")
	return query.RejectCode(head, sub)
}

// originOf classifies a decision from its direction and gate, the view's rule.
func originOf(direction string, g gate) string {
	switch {
	case direction == "" || direction == domain.JevDirectionNone:
		return originNoDirection
	case g.passed:
		return originEntered
	case g.count == 0:
		return originNoSignal
	case g.lastWhy == riskRejectedHead || strings.HasPrefix(g.lastWhy, riskRejectedHead+":"):
		return originRiskReject
	}
	return originGuardSkip
}

// attachGates fills the gate columns from the decisions' trade signals (one batched read).
func attachGates(ctx context.Context, q query.Q, from, to string, rows []*row, byID map[int64]*row) error {
	res, err := q.QueryContext(ctx, `
SELECT s.jev_decision_id, s.id, s.risk_passed = 1, COALESCE(s.reject_reason, '')
FROM jev_decisions d
JOIN trade_signals s ON s.jev_decision_id = d.id
WHERE`+scope, from, to)
	if err != nil {
		return fmt.Errorf("datasetexport: read trade signals: %w", err)
	}
	defer func() { _ = res.Close() }()
	gates := map[int64]*gate{}
	for res.Next() {
		var (
			id, sid int64
			passed  bool
			why     string
		)
		if err := res.Scan(&id, &sid, &passed, &why); err != nil {
			return fmt.Errorf("datasetexport: scan trade signal: %w", err)
		}
		g := gates[id]
		if g == nil {
			g = &gate{}
			gates[id] = g
		}
		g.count++
		g.passed = g.passed || passed
		if sid > g.lastID {
			g.lastID, g.lastPass, g.lastWhy = sid, passed, why
		}
	}
	if err := res.Err(); err != nil {
		return fmt.Errorf("datasetexport: read trade signals: %w", err)
	}
	for _, r := range rows {
		g := gates[r.id]
		if g == nil {
			g = &gate{}
		}
		p := off.gate
		r.cells[p+gHasSignal] = boolCell(g.count > 0)
		r.cells[p+gSignalCount] = strconv.Itoa(g.count)
		r.cells[p+gRiskPassed] = boolCell(g.passed)
		if g.count > 0 && !g.lastPass {
			r.cells[p+gRejectCode] = rejectCode(g.lastWhy)
		}
		r.cells[p+gOrigin] = originOf(r.cells[off.decision], *g) // off.decision is answer_direction
	}
	return nil
}

// versionCount is one shadow version or trial arm with the decisions it answered.
type versionCount struct {
	name string
	n    int
}

// topSlots picks the versions that get columns: most answered first, ties by name, at most limit.
// omitted is how many more there were.
func topSlots(counts []versionCount, limit int) (names []string, omitted int) {
	slices.SortFunc(counts, func(a, b versionCount) int { return cmp.Or(cmp.Compare(b.n, a.n), cmp.Compare(a.name, b.name)) })
	for i, c := range counts {
		if i < limit {
			names = append(names, c.name)
		}
	}
	return names, max(len(counts)-limit, 0)
}

// answerSource is a table of per-decision answers pivoted into slot columns.
type answerSource struct {
	table, key   string // the table and its version / arm column
	statusColumn string // a status column ("" when the table has none)
	max, start   int    // the slot cap and the group's first cell
	width        int    // cells per slot
}

// attachAnswers pivots the shadow (per prompt version) and trial (per arm) answers into
// their fixed slot columns: direction, confidence and entry quality / status only, never a
// body. The slot versions are the answerSource.max most answered of the period.
func attachAnswers(ctx context.Context, q query.Q, from, to string, byID map[int64]*row) (shadow, trial []string, shadowOmitted, trialOmitted int, err error) {
	shadow, shadowOmitted, err = attachSlots(ctx, q, from, to, byID, answerSource{
		table: "jev_shadow_decisions", key: "prompt_version", max: MaxShadowVersions, start: off.shadow, width: 4})
	if err != nil {
		return nil, nil, 0, 0, err
	}
	trial, trialOmitted, err = attachSlots(ctx, q, from, to, byID, answerSource{
		table: "jev_trial_decisions", key: "arm_id", statusColumn: "status", max: MaxTrialArms, start: off.trial, width: 4})
	return shadow, trial, shadowOmitted, trialOmitted, err
}

func attachSlots(ctx context.Context, q query.Q, from, to string, byID map[int64]*row, src answerSource) ([]string, int, error) {
	t := "s." + src.key
	groups, err := q.QueryContext(ctx, `
SELECT `+t+`, COUNT(*) FROM jev_decisions d
JOIN `+src.table+` s ON s.jev_decision_id = d.id
WHERE`+scope+` GROUP BY `+t, from, to)
	if err != nil {
		return nil, 0, fmt.Errorf("datasetexport: read %s versions: %w", src.table, err)
	}
	var counts []versionCount
	for groups.Next() {
		var c versionCount
		if err := groups.Scan(&c.name, &c.n); err != nil {
			_ = groups.Close()
			return nil, 0, fmt.Errorf("datasetexport: scan %s version: %w", src.table, err)
		}
		counts = append(counts, c)
	}
	if err := groups.Err(); err != nil {
		_ = groups.Close()
		return nil, 0, fmt.Errorf("datasetexport: read %s versions: %w", src.table, err)
	}
	_ = groups.Close()
	names, omitted := topSlots(counts, src.max)
	if len(names) == 0 {
		return nil, omitted, nil
	}

	statusExpr := "''"
	if src.statusColumn != "" {
		statusExpr = "CASE WHEN s.status IN ('ok', 'failed', 'refused', 'partial') THEN s.status ELSE '' END"
	}
	args := []any{from, to}
	marks := make([]string, len(names))
	for i, n := range names {
		args, marks[i] = append(args, n), "?"
	}
	res, err := q.QueryContext(ctx, `
SELECT s.jev_decision_id, `+t+`, `+statusExpr+`,
	CASE WHEN s.direction IN ('LONG', 'SHORT', 'NONE') THEN s.direction END,
	CASE WHEN `+numGuard("s.confidence")+` THEN s.confidence END,
	CASE WHEN s.entry_quality IN ('poor', 'fair', 'good', 'strong', 'exceptional') THEN s.entry_quality END
FROM jev_decisions d
JOIN `+src.table+` s ON s.jev_decision_id = d.id
WHERE`+scope+` AND `+t+` IN (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("datasetexport: read %s answers: %w", src.table, err)
	}
	defer func() { _ = res.Close() }()
	for res.Next() {
		var (
			id                 int64
			name, status       string
			direction, quality sql.NullString
			confidence         sql.NullFloat64
		)
		if err := res.Scan(&id, &name, &status, &direction, &confidence, &quality); err != nil {
			return nil, 0, fmt.Errorf("datasetexport: scan %s answer: %w", src.table, err)
		}
		r, slot := byID[id], slices.Index(names, name)
		if r == nil || slot < 0 {
			continue
		}
		p := src.start + slot*src.width
		r.cells[p] = name
		if src.statusColumn != "" { // trial: arm_id, status, direction, confidence
			r.cells[p+1], r.cells[p+2], r.cells[p+3] = status, direction.String, cellOf(&confidence)
		} else { // shadow: version, direction, confidence, entry_quality
			r.cells[p+1], r.cells[p+2], r.cells[p+3] = direction.String, cellOf(&confidence), quality.String
		}
	}
	if err := res.Err(); err != nil {
		return nil, 0, fmt.Errorf("datasetexport: read %s answers: %w", src.table, err)
	}
	return names, omitted, nil
}

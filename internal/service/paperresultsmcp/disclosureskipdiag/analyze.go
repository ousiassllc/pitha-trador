package disclosureskipdiag

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

const (
	skipText    = "判断の時刻以前の開示のうち、判断日の当日か直前の東証営業日の15:00以降に出た決算短信・業績予想の修正（上方・下方とも）が 1 件以上ある"
	noSkipText  = "やのしんの取得が窓（直前の東証営業日 15:00 JST から判断時刻まで）を覆っていて、対象の開示が 0 件"
	unknownText = "取得が失敗・欠け・件数上限で窓に届かず、対象の開示も見つからなかった。「該当なし」ではない"
	selectText  = "判断ごとに label_version=2 で net_return_bps がある行のうち新しい1行（entry_quality_whatif・メタモデルの学習行と同じ規則）。ラベルのない判断は unlabeled に数え、集計に入れない"
	winText     = "net_return_bps > 0（コスト控除後。entry_quality_whatif・メタモデルの学習と同じ定義）。win_rate = wins / labeled。jev_calibration の hit_rate（was_direction_correct）とは別の定義"
)

// scope is the SQL condition on jev_decisions.question_version of a.
func scope(a Args) (string, []any) {
	if a.PromptVersion != "" {
		return "d.question_version = ?", []any{a.PromptVersion}
	}
	return sqlutil.NotIn("d.question_version", a.Foreign)
}

// Analyze summarizes the entry-candidate decisions of [r.From, r.To). It reads only.
func Analyze(ctx context.Context, q query.Q, r query.Range, a Args) (Data, error) {
	from, to := r.Bounds()
	out := Data{
		RowsFrom: from, RowsTo: to,
		PromptVersions: PromptScope{Only: a.PromptVersion, Excluded: append([]string{}, a.Foreign...)},
		Rule:           Rule{Versions: []int{}, Skip: skipText, NoSkip: noSkipText, Unknown: unknownText},
		UnknownReasons: []Bucket{}, SkipKinds: []Bucket{},
		Labels: Labels{LabelVersion: domain.CalibrationLabelVersionTripleBarrier, Selection: selectText, Win: winText},
	}
	if a.PromptVersion != "" {
		out.PromptVersions.Excluded = []string{}
	}
	cond, condArgs := scope(a)
	if cond != "" {
		cond = " AND " + cond
	}
	where := `d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?` + cond
	args := append([]any{from, to}, condArgs...)

	if err := groups(ctx, q, &out, where, args); err != nil {
		return Data{}, err
	}
	var err error
	if out.UnknownReasons, err = buckets(ctx, q, `
SELECT r.unknown_reason, COUNT(*) FROM disclosure_skip_records r JOIN jev_decisions d ON d.id = r.jev_decision_id
WHERE r.verdict = 'unknown' AND `+where+` GROUP BY r.unknown_reason ORDER BY r.unknown_reason`, args); err != nil {
		return Data{}, err
	}
	if out.SkipKinds, err = buckets(ctx, q, `
SELECT m.kind, COUNT(DISTINCT m.record_id) FROM disclosure_skip_matches m
JOIN disclosure_skip_records r ON r.id = m.record_id JOIN jev_decisions d ON d.id = r.jev_decision_id
WHERE `+where+` GROUP BY m.kind ORDER BY m.kind`, args); err != nil {
		return Data{}, err
	}
	if out.Rule.Versions, err = ruleVersions(ctx, q, where, args); err != nil {
		return Data{}, err
	}
	return out, nil
}

// groups fills Population and Verdicts: one pass over the period's decisions,
// each joined to its record (none: not recorded) and its newest label.
func groups(ctx context.Context, q query.Q, out *Data, where string, args []any) error {
	rows, err := q.QueryContext(ctx, `
SELECT r.verdict, COUNT(*), COUNT(o.net_return_bps), COALESCE(SUM(o.net_return_bps > 0), 0), COALESCE(SUM(o.net_return_bps), 0)
FROM jev_decisions d
LEFT JOIN disclosure_skip_records r ON r.jev_decision_id = d.id
LEFT JOIN calibration_outcomes o ON o.id = (SELECT MAX(o2.id) FROM calibration_outcomes o2
	WHERE o2.jev_decision_id = d.id AND o2.label_version = ? AND o2.net_return_bps IS NOT NULL)
WHERE `+where+`
GROUP BY r.verdict`, append([]any{domain.CalibrationLabelVersionTripleBarrier}, args...)...)
	if err != nil {
		return fmt.Errorf("query: disclosure skip groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			verdict              sql.NullString
			count, labeled, wins int
			sum                  float64
		)
		if err := rows.Scan(&verdict, &count, &labeled, &wins, &sum); err != nil {
			return fmt.Errorf("query: scan disclosure skip group: %w", err)
		}
		out.Population.EntryCandidates += count
		if !verdict.Valid {
			out.Population.NotRecorded += count
			continue
		}
		out.Population.Recorded += count
		g := group(count, labeled, wins, sum)
		switch disclosureskip.Verdict(verdict.String) {
		case disclosureskip.VerdictSkip:
			out.Verdicts.Skip = g
		case disclosureskip.VerdictNoSkip:
			out.Verdicts.NoSkip = g
		case disclosureskip.VerdictUnknown:
			out.Verdicts.Unknown = g
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query: disclosure skip groups: %w", err)
	}
	return nil
}

func group(count, labeled, wins int, sum float64) Group {
	g := Group{Count: count, Labeled: labeled, Unlabeled: count - labeled, Wins: wins}
	if labeled == 0 {
		return g
	}
	rate := float64(wins) / float64(labeled)
	g.WinRate = &rate
	g.NetReturn = &NetReturn{Mean: sum / float64(labeled), Sum: sum}
	return g
}

func buckets(ctx context.Context, q query.Q, stmt string, args []any) ([]Bucket, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("query: disclosure skip buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Code, &b.Count); err != nil {
			return nil, fmt.Errorf("query: scan disclosure skip bucket: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: disclosure skip buckets: %w", err)
	}
	return out, nil
}

func ruleVersions(ctx context.Context, q query.Q, where string, args []any) ([]int, error) {
	rows, err := q.QueryContext(ctx, `
SELECT DISTINCT r.rule_version FROM disclosure_skip_records r JOIN jev_decisions d ON d.id = r.jev_decision_id
WHERE `+where+` ORDER BY r.rule_version`, args...)
	if err != nil {
		return nil, fmt.Errorf("query: disclosure skip rule versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []int{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("query: scan disclosure skip rule version: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: disclosure skip rule versions: %w", err)
	}
	return out, nil
}

package trialquery

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The fixed definitions of the skip counts jev_trial_compare returns next to the
// cohort premise (issue #1062).
const (
	// SkippedDefinition says what a skip is and where it is not counted.
	SkippedDefinition = "見送り = お試しがライブの trader 判断に対してアームを呼ばなかったこと（理由別。同時実行枠の満杯・最短間隔・日次上限・キーなし・プロンプト未登録・停止中・429/529 の待機など）。" +
		"1 判断 × 1 アームにつき 1 件で、行（答え・費用・status）は無く、成功・失敗・欠測の行としては数えない。見送りが起きたアームの判断は cohort に入らない。" +
		"欠測率（missing_rate）の分母にも分子にも入れない（分母は呼び出した行だけ）。asked_share = 呼んだ行 /（呼んだ行 + 見送り）は欠測率とは別の指標で、呼べた割合を表す"
	// DroppedDecisionsDefinition names the decision-level counts of Skipped.
	DroppedDecisionsDefinition = "recorded_decisions = 選んだアームのどれかに行か見送りの記録がある判断の数 = cohort_decisions + dropped_decisions + outside_cohort_without_skip。" +
		"dropped_decisions = 選んだアームのどれかが見送った判断（cohort の外。理由は by_reason）。" +
		"outside_cohort_without_skip = 一部のアームにだけ行があり、見送りの記録も無い判断（アームを途中で足した・停止やシャットダウンで記録が残らなかった等）。" +
		"お試しが無効だった・設定を読めなかったなど、行も見送りの記録も一切無い判断はどの数にも入らない"
)

// skipRow is one jev_trial_skips record of a live trader decision of the period.
type skipRow struct {
	decisionID int64
	arm        string
	reason     string
}

// ArmSkips is one arm's skips next to the rows it was asked for. AskedShare is
// asked / (asked + skipped), nil when both are 0; it is not the missing rate.
type ArmSkips struct {
	ArmID      string         `json:"arm_id"`
	Asked      int            `json:"asked"`
	Skipped    int            `json:"skipped"`
	AskedShare *float64       `json:"asked_share"`
	ByReason   map[string]int `json:"by_reason"`
}

// Skipped is the live decisions the comparison did not cover, by reason, next to
// the cohort premise "only decisions every chosen arm was asked". Skips are never
// added to a missing rate's denominator (CountedInMissingRate is always false).
type Skipped struct {
	Definition                 string `json:"definition"`
	DroppedDecisionsDefinition string `json:"dropped_decisions_definition"`
	// CountedInMissingRate is always false: a skip is not a row, so it is neither
	// a failure nor part of the denominator of missing_rate.
	CountedInMissingRate bool `json:"counted_in_missing_rate"`
	// RecordedDecisions = CohortDecisions + DroppedDecisions + OutsideCohortWithoutSkip.
	RecordedDecisions        int `json:"recorded_decisions"`
	CohortDecisions          int `json:"cohort_decisions"`
	DroppedDecisions         int `json:"dropped_decisions"`
	OutsideCohortWithoutSkip int `json:"outside_cohort_without_skip"`
	// ArmDecisionsSkipped counts the skips of the compared arms (one per
	// decision and arm); ByReason splits it, every reason always present.
	ArmDecisionsSkipped int            `json:"arm_decisions_skipped"`
	ByReason            map[string]int `json:"by_reason"`
	PerArm              []ArmSkips     `json:"per_arm"`
	// ArmsWithoutRows are arms that only have skips in the period (when the arms
	// were not chosen): they are not among the compared arms, so their skips do
	// not change the cohort, but they are not left out of sight either.
	ArmsWithoutRows []ArmSkips `json:"arms_without_rows"`
}

// reasonCounts is a map with every skip reason present (0 when none).
func reasonCounts() map[string]int {
	m := make(map[string]int, len(jevtrial.SkipReasons))
	for _, r := range jevtrial.SkipReasons {
		m[r] = 0
	}
	return m
}

// loadSkips reads the skip records of the live trader decisions of the period
// (decision timestamp in [from, to)), of every arm.
func loadSkips(ctx context.Context, q query.Q, from, to string) ([]skipRow, error) {
	rows, err := q.QueryContext(ctx, `
SELECT s.jev_decision_id, s.arm_id, s.reason
FROM jev_trial_skips s JOIN jev_decisions d ON d.id = s.jev_decision_id
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?
ORDER BY s.jev_decision_id, s.arm_id LIMIT ?`, from, to, MaxRows+1)
	if err != nil {
		return nil, fmt.Errorf("query: jev trial skips: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []skipRow
	for rows.Next() {
		var s skipRow
		if err := rows.Scan(&s.decisionID, &s.arm, &s.reason); err != nil {
			return nil, fmt.Errorf("query: scan jev trial skips: %w", err)
		}
		if len(out) == MaxRows {
			return nil, query.ErrLimitExceeded
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: jev trial skips: %w", err)
	}
	return out, nil
}

// buildSkipped relates the skip records to the cohort. arms are the compared arms
// (the cohort's), chosen says whether the caller named them; decisions are the
// decisions that have a row of the compared arms and cohort the ones asked of all
// of them. asked[i] is arm i's row count in the period.
func buildSkipped(skips []skipRow, arms []string, chosen bool, decisions, cohort []*decision, asked []int) Skipped {
	out := Skipped{
		Definition: SkippedDefinition, DroppedDecisionsDefinition: DroppedDecisionsDefinition,
		CohortDecisions: len(cohort), ByReason: reasonCounts(), PerArm: make([]ArmSkips, len(arms)), ArmsWithoutRows: []ArmSkips{},
	}
	armIndex := map[string]int{}
	for i, a := range arms {
		armIndex[a] = i
		out.PerArm[i] = ArmSkips{ArmID: a, Asked: asked[i], ByReason: reasonCounts()}
	}
	cohortIDs := map[int64]bool{}
	for _, d := range cohort {
		cohortIDs[d.id] = true
	}
	dropped := map[int64]bool{}
	without := map[string]*ArmSkips{}
	var withoutOrder []string
	for _, s := range skips {
		if i, ok := armIndex[s.arm]; ok {
			out.ArmDecisionsSkipped++
			out.ByReason[s.reason]++
			out.PerArm[i].Skipped++
			out.PerArm[i].ByReason[s.reason]++
			if !cohortIDs[s.decisionID] {
				dropped[s.decisionID] = true
			}
			continue
		}
		if chosen {
			continue // an arm the caller did not choose
		}
		a := without[s.arm]
		if a == nil {
			a = &ArmSkips{ArmID: s.arm, ByReason: reasonCounts()}
			without[s.arm] = a
			withoutOrder = append(withoutOrder, s.arm)
		}
		a.Skipped++
		a.ByReason[s.reason]++
	}
	out.DroppedDecisions = len(dropped)
	withRow := map[int64]bool{}
	for _, d := range decisions {
		withRow[d.id] = true
		if !cohortIDs[d.id] && !dropped[d.id] {
			out.OutsideCohortWithoutSkip++
		}
	}
	out.RecordedDecisions = len(withRow)
	for id := range dropped {
		if !withRow[id] {
			out.RecordedDecisions++
		}
	}
	for i := range out.PerArm {
		a := &out.PerArm[i]
		a.AskedShare = ratio(a.Asked, a.Asked+a.Skipped)
	}
	for _, id := range withoutOrder {
		a := without[id]
		a.AskedShare = ratio(0, a.Skipped)
		out.ArmsWithoutRows = append(out.ArmsWithoutRows, *a)
	}
	return out
}

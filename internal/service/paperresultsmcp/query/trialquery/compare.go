package trialquery

import (
	"context"
	"slices"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Definitions are the fixed definitions jev_trial_compare returns, so a reader
// need not guess what a denominator is.
var Definitions = map[string]string{
	"cohort":         "期間内のライブの trader 判断のうち、選んだアームすべてに聞いた判断（行の status は問わない）。失敗・拒否・一部拒否のアームの行は「答えなし」として分母に入る",
	"common_success": "cohort のうち、選んだアームすべての status が ok の判断。参考値（informational）で、順位の元にはしない",
	"missing_rate":   "(failed + refused + partial) / 総呼び出し数。分母に失敗・拒否・一部拒否を含む",
	"over_cohort":    "分母 = cohort（結果の記録がある判断）。答えなし・NONE・採点できない方向を含む。順位はこちらで付く",
	"over_answered":  "分母 = 採点できた答え（方向が LONG/SHORT で、ライブの方向と同じかライブが NONE）。答えなし・NONE を含まないので、答えた分だけが良く見える。参考値",
	"none":           "NONE は方向の結果が無いので当たり外れに数えない（none_count）。方向を補完しない。分母（cohort）には入る",
	"hit":            "方向で符号を付けた future_return（既存の calibration_outcomes。コスト控除前）が 0 を超えたとき。ライブの方向のラベルなので、ライブと逆の方向の答えは採点しない（excluded_disagreement）",
	"rank":           "over_cohort の当たり率の高い順の並べ替え（同率は同順位。ラベルはライブの方向のものなので、ライブと逆向きの答えは採点されず当たり率にはライブとの一致度も含まれる。比べる基準は live_baseline）。アームが 1 つだけのときは null。結果の記録のある判断が最低 " + strconv.Itoa(MinOutcomeRecordsForRank) + " 件ないと null。昇格・採用の推奨ではない",
}

// ArmReport is one arm of jev_trial_compare.
type ArmReport struct {
	ArmID string `json:"arm_id"`
	// PeriodRows counts every row of the arm in the period; CohortRows only the
	// rows of cohort decisions.
	PeriodRows   RowCounts       `json:"period_rows"`
	CohortRows   RowCounts       `json:"cohort_rows"`
	Direction    DirectionCounts `json:"direction"`
	Agreement    Agreement       `json:"direction_agreement"`
	EntryQuality []QualityCount  `json:"entry_quality"`
}

// Cohort describes the cohort and its common-success subset.
type Cohort struct {
	Definition              string   `json:"definition"`
	CommonSuccessDefinition string   `json:"common_success_definition"`
	Size                    int      `json:"size"`
	CommonSuccessCount      int      `json:"common_success_count"`
	CommonSuccessRate       *float64 `json:"common_success_rate"`
	// Rows counts the arms' rows of the cohort decisions (all arms together).
	Rows RowCounts `json:"rows"`
}

// Fairness is what keeps the comparison from being a fair same-input one. The
// rows themselves are counted in Cohort.Rows and ArmReport.CohortRows (never
// excluded); Caveats names the conditions present among the cohort's rows.
type Fairness struct {
	CohortSize int      `json:"cohort_size"`
	Caveats    []string `json:"caveats"`
}

// ArmOutcome is one arm in an outcome slice. CommonSuccess is the same metrics
// on the common-success subset (informational, no rank).
type ArmOutcome struct {
	ArmID         string             `json:"arm_id"`
	Rank          *int               `json:"rank"`
	Cohort        OutcomeStats       `json:"cohort"`
	CommonSuccess CommonSuccessStats `json:"common_success"`
}

// CommonSuccessStats is OutcomeStats over the common-success subset, labelled informational.
type CommonSuccessStats struct {
	Informational bool `json:"informational"`
	OutcomeStats
}

// OutcomeSlice is the outcomes of one horizon and label version. Labels of
// different versions define "correct" differently and are never mixed.
type OutcomeSlice struct {
	HorizonMinutes int `json:"horizon_minutes"`
	LabelVersion   int `json:"label_version"`
	// LiveBaseline is the live Jev's own score of the same cohort decisions, so
	// an arm's hit rate can be read against it: the labels belong to the live
	// direction, so an arm that disagrees with the live direction is not scored
	// (excluded_disagreement) and a rate over the cohort also measures agreement.
	LiveBaseline LiveBaseline `json:"live_baseline"`
	Arms         []ArmOutcome `json:"arms"`
}

// Comparison is jev_trial_compare's data.
type Comparison struct {
	Notice              string            `json:"notice"`
	Definitions         map[string]string `json:"definitions"`
	Arms                []string          `json:"arms"`
	Cohort              Cohort            `json:"cohort"`
	PerArm              []ArmReport       `json:"per_arm"`
	Fairness            Fairness          `json:"fairness"`
	Outcomes            []OutcomeSlice    `json:"outcomes"`
	CounterfactualProxy Proxy             `json:"counterfactual_proxy"`
}

// Compare compares the trial arms on the live trader decisions of r (by the
// live decision's timestamp). arms are the arm ids to compare (empty: every arm
// with a row in the period). The ranking, agreement and calibration are over
// the cohort - the decisions asked of every selected arm, whatever the status
// of the rows - so an arm that fails or refuses is not flattered by dropping
// its misses; the same metrics over the common-success subset are labelled
// informational. Nothing is imputed: a NONE is not given a direction.
func Compare(ctx context.Context, q query.Q, r query.Range, arms []string) (Comparison, error) {
	if err := ValidateArms(arms); err != nil {
		return Comparison{}, err
	}
	from, to := r.Bounds()
	flat, err := loadRows(ctx, q, from, to, arms)
	if err != nil {
		return Comparison{}, err
	}
	arms, decisions, err := groupDecisions(flat, slices.Clone(arms))
	if err != nil {
		return Comparison{}, err
	}
	out := Comparison{
		Notice: Notice, Definitions: Definitions, Arms: arms, PerArm: make([]ArmReport, len(arms)), Outcomes: []OutcomeSlice{},
		Fairness: Fairness{Caveats: []string{}},
	}
	var cohort, common []*decision
	for _, d := range decisions {
		if !slices.Contains(d.rows, nil) && len(arms) > 0 {
			cohort = append(cohort, d)
			if allOK(d) {
				common = append(common, d)
			}
		}
	}
	out.Cohort = Cohort{
		Definition: Definitions["cohort"], CommonSuccessDefinition: Definitions["common_success"], Size: len(cohort),
		CommonSuccessCount: len(common), CommonSuccessRate: ratio(len(common), len(cohort)), Rows: newRowCounts(),
	}
	for i, arm := range arms {
		rep := ArmReport{ArmID: arm, PeriodRows: newRowCounts(), CohortRows: newRowCounts()}
		for _, d := range decisions {
			if row := d.rows[i]; row != nil {
				rep.PeriodRows.add(row)
			}
		}
		for _, d := range cohort {
			rep.CohortRows.add(d.rows[i])
			out.Cohort.Rows.add(d.rows[i])
		}
		rep.PeriodRows.finish()
		rep.CohortRows.finish()
		rep.Direction, rep.Agreement, rep.EntryQuality = armDirection(cohort, i)
		out.PerArm[i] = rep
	}
	out.Cohort.Rows.finish()
	out.Fairness.CohortSize = len(cohort)
	out.Fairness.Caveats = caveats(out.Cohort.Rows)

	outcomes, err := loadOutcomes(ctx, q, from, to)
	if err != nil {
		return Comparison{}, err
	}
	out.Outcomes = outcomeSlices(arms, cohort, common, outcomes)
	labels, err := loadCounterfactual(ctx, q, from, to)
	if err != nil {
		return Comparison{}, err
	}
	out.CounterfactualProxy = buildProxy(arms, cohort, labels)
	return out, nil
}

func allOK(d *decision) bool {
	for _, r := range d.rows {
		if r.status != jevtrial.StatusOK {
			return false
		}
	}
	return true
}

// caveats names the fairness conditions present in rows.
func caveats(rows RowCounts) []string {
	out := []string{}
	if rows.TruncationSuspected > 0 {
		out = append(out, "truncation_suspected")
	}
	if rows.RAGIncludesLiveModelAnswers > 0 {
		out = append(out, "rag_includes_live_model_answers")
	}
	if rows.LLMCutoffStatus[jevtrial.CutoffUnverified] > 0 {
		out = append(out, "llm_cutoff_unverified")
	}
	return out
}

// outcomeSlices scores every arm on every (horizon, label version) that has
// outcome records for the cohort, ordered by horizon then label version.
func outcomeSlices(arms []string, cohort, common []*decision, outcomes map[outcomeKey]map[int64]float64) []OutcomeSlice {
	keys := make([]outcomeKey, 0, len(outcomes))
	for k := range outcomes {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b outcomeKey) int {
		if a.horizon != b.horizon {
			return a.horizon - b.horizon
		}
		return a.labelVersion - b.labelVersion
	})
	out := []OutcomeSlice{}
	for _, k := range keys {
		returns := outcomes[k]
		stats := make([]OutcomeStats, len(arms))
		for i := range arms {
			stats[i] = scoreOutcomes(cohort, i, returns)
		}
		if len(arms) == 0 || stats[0].Records == 0 {
			continue // no cohort decision has a record in this slice
		}
		ranks := rankByHitRateOverCohort(stats)
		slice := OutcomeSlice{HorizonMinutes: k.horizon, LabelVersion: k.labelVersion, LiveBaseline: scoreLive(cohort, returns), Arms: make([]ArmOutcome, len(arms))}
		for i, arm := range arms {
			slice.Arms[i] = ArmOutcome{
				ArmID: arm, Rank: ranks[i], Cohort: stats[i],
				CommonSuccess: CommonSuccessStats{Informational: true, OutcomeStats: scoreOutcomes(common, i, returns)},
			}
		}
		out = append(out, slice)
	}
	return out
}

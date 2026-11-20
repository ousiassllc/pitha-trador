package replaytool

import (
	"context"
	"errors"
	"fmt"

	domainreplay "github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

// ResultsNotice is the fixed caveat of replay_results.
const ResultsNotice = "リプレイの結果は研究用の記録で、ライブの判断・取引・Risk・学習・RAG には使われない。comparison は jev_trial_compare と同じ指標（共通の成功数 common_success_count・" +
	"欠測率 missing_rate・ラベルでの当たり率と live_baseline・見送りの反実仮想ラベルのコスト控除後 net_return_bps の proxy）を、この run の行とライブの判断を同じ行どうしで比べたもの。" +
	"cohort は選んだアームすべてに聞いた判断だけなので、学習カットオフ後の判断だけを聞いたアームがあると cohort はその分だけ小さくなる（アームごとの excluded_before_cutoff は replay_estimate）。" +
	"llm_cutoff_status が unverified（未確認）のアームの行は参考扱い（reference_only）で、学習データに入っていた可能性を排除できない。" +
	"送った入力は保存された入力そのまま（過去の類似事例 RAG は足さない）なので、ライブのお試し（RAG あり）とは入力が違う。cost は呼び出しにかかった API 費用（counted は上限の判定に数えた額）"

// ArmResultCost is one arm's rows and API cost in a run.
type ArmResultCost struct {
	ArmID           string `json:"arm_id"`
	LLMCutoffStatus string `json:"llm_cutoff_status"`
	LLMCutoffLabel  string `json:"llm_cutoff_label"`
	Rows            int    `json:"rows"`
	// ReferenceOnlyRows counts the rows marked reference-only.
	ReferenceOnlyRows int `json:"reference_only_rows"`
	// CostUSD sums the reported or list-price-estimated cost of the rows that have
	// one; UnknownCostRows counts the rows with neither (their cost is not 0 USD,
	// it is unknown: CountedCostUSD counts them at the worst-case price).
	CostUSD         float64 `json:"cost_usd"`
	UnknownCostRows int     `json:"unknown_cost_rows"`
	CountedCostUSD  float64 `json:"counted_cost_usd"`
}

// ResultsView is replay_results' data.
type ResultsView struct {
	Notice string  `json:"notice"`
	Run    RunView `json:"run"`
	// ReferenceOnly is true when any arm of the run is unverified.
	ReferenceOnly bool                  `json:"reference_only"`
	Cost          []ArmResultCost       `json:"cost"`
	Comparison    trialquery.Comparison `json:"comparison"`
}

// Results is replay_results: the metrics of jev_trial_compare over one run's
// rows (arms: a subset of the run's arms, empty for all), the per-arm cost and
// the run. It never returns response bodies or error texts. An unknown run is
// ErrNotFound.
func Results(ctx context.Context, q query.Q, runID int64, arms []string) (ResultsView, error) {
	run, err := repo.GetRun(ctx, q, runID)
	if errors.Is(err, repo.ErrRunNotFound) {
		return ResultsView{}, ErrNotFound
	}
	if err != nil {
		return ResultsView{}, err
	}
	cmp, err := trialquery.CompareReplay(ctx, q, query.Range{From: run.PeriodFrom, To: run.PeriodTo}, run.ID, arms)
	if err != nil {
		return ResultsView{}, err
	}
	cost, err := armCosts(ctx, q, run.ID)
	if err != nil {
		return ResultsView{}, err
	}
	v := ResultsView{Notice: ResultsNotice, Run: View(run), Cost: cost, Comparison: cmp}
	for _, a := range v.Run.Arms {
		if a.ReferenceOnly {
			v.ReferenceOnly = true
		}
	}
	return v, nil
}

func armCosts(ctx context.Context, q query.Q, runID int64) ([]ArmResultCost, error) {
	rows, err := q.QueryContext(ctx, `
SELECT arm_id, llm_cutoff_status, COUNT(*), COALESCE(SUM(reference_only), 0), COALESCE(SUM(cost_usd), 0),
	COALESCE(SUM(cost_source = 'unknown'), 0), COALESCE(SUM(counted_cost_usd), 0)
FROM jev_replay_results WHERE run_id = ? GROUP BY arm_id, llm_cutoff_status ORDER BY arm_id, llm_cutoff_status`, runID)
	if err != nil {
		return nil, fmt.Errorf("query: jev replay cost: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ArmResultCost{}
	for rows.Next() {
		var c ArmResultCost
		if err := rows.Scan(&c.ArmID, &c.LLMCutoffStatus, &c.Rows, &c.ReferenceOnlyRows, &c.CostUSD, &c.UnknownCostRows, &c.CountedCostUSD); err != nil {
			return nil, fmt.Errorf("query: scan jev replay cost: %w", err)
		}
		c.LLMCutoffLabel = domainreplay.CutoffLabel(c.LLMCutoffStatus)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: jev replay cost: %w", err)
	}
	return out, nil
}

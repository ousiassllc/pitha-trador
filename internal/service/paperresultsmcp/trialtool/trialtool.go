// Package trialtool holds the arguments, descriptions and entry points of the
// two read-only model-comparison tools of the Paper results MCP (issue #1060):
// jev_trial_compare and jev_trial_cost (docs/api/endpoints/paper-results-mcp-jev-trial.md).
// The SQL and the aggregation are query/trialquery's. Neither tool writes
// anything: the trial's switch (jev.trial.enabled), its arms and its caps are
// the Settings screen's, not on the MCP's write whitelist.
package trialtool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

// CompareDescription is jev_trial_compare's description.
const CompareDescription = "意思決定モデルの比較（お試し）の読み取り専用の集計: 期間（ライブの trader 判断の時刻）とアーム（省略は期間内の全アーム）を指定し、" +
	"cohort（選んだアームすべてに聞いた判断。行の status は問わない）・common_success_count（全アームが ok だった判断の数）、" +
	"アームごとの欠測率 missing_rate（分母に失敗・拒否・一部拒否を含む）と failure_code の件数、" +
	"方向の分布（NONE は none_count の別枠。方向を補完しない）・ライブとの方向一致・entry_quality の分布、" +
	"既存の結果の記録（calibration_outcomes）での当たり率を over_cohort（分母＝cohort。答えなしを含む）と over_answered（採点できた答えだけ。参考）で、" +
	"common_success の部分集合の同じ指標を informational として返す。rank は over_cohort の並べ替えで、昇格・採用の推奨ではない（成功した行だけで順位は作らない）。" +
	"truncation_suspected・rag_includes_live_model_answers の行・llm_cutoff_status の内訳は除外せず件数で出す。" +
	"見送りの反実仮想ラベル（既存）は NONE・答えなしの判断について counterfactual_proxy（proxy: true）に別に出し、実現した結果の指標に混ぜない。" +
	"cohort から落ちた判断（お試しが呼ばなかった見送り。同時実行枠・最短間隔・日次上限・キーなし・プロンプト未登録・停止中・429/529 の待機）の件数を fairness.skipped に理由別に出す（欠測率の分母には入れない）。" +
	"多数決・合議・勝者・推奨の項目は無い。エラー本文・応答本文は返さない"

// CostDescription is jev_trial_cost's description.
const CostDescription = "意思決定モデルの比較（お試し）の費用の読み取り専用の集計: 期間の JST の日ごと・合計・アームごとに、呼び出し数と " +
	"reported（応答が報告した費用）・list_price_estimate（定価×トークンの推定）・unknown（どちらも無い。実費は null で 0 円ではない。" +
	"unknown_counted_usd は上限の判定に数えた最悪値側の額で、他の合計に混ぜない）を出所別に、counted_total_usd（上限の判定に数えた額）。" +
	"limits に、いまの全体上限・アームごとの上限と今日の数えた額・止まった記録（理由と日だけ）・失敗回数・429/529 の止まり・有効かどうか（enabled）・今日の見送り（呼ばなかった比較）の件数 skipped_today（理由別）を読み取りで返す。" +
	"設定の変更はできない（有効化・上限・アームは Settings 画面だけ）。API キー・エラー本文・応答本文は返さない"

// SchemaProperties are the tools' own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"arms": map[string]any{
			"type": "array", "maxItems": trialquery.MaxArms, "uniqueItems": true,
			"items":       map[string]any{"type": "string", "minLength": 1, "maxLength": 48},
			"description": "対象のアーム id（jev.trial.arms の id。例 or-clef-flash-v3）。省略は期間内に行のある全アーム",
		},
	}
}

// Request is the tools' arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and Arms. Decoding rejects an unknown
// field and an argument Validate refuses.
type Request struct {
	From *string  `json:"from"`
	To   *string  `json:"to"`
	Arms []string `json:"arms"`
}

// UnmarshalJSON decodes strictly and validates.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	if err := trialquery.ValidateArms(r.Arms); err != nil {
		return fmt.Errorf("trialtool: arms: %w", err)
	}
	return nil
}

// RunFunc runs a tool over the resolved period and returns its data and the row
// count to log.
type RunFunc func(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error)

// RunCompare is jev_trial_compare.
func RunCompare(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	c, err := trialquery.Compare(ctx, q, r, req.Arms)
	return c, c.Cohort.Size, err
}

// RunCost is jev_trial_cost.
func RunCost(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error) {
	c, err := trialquery.Cost(ctx, q, r, now, req.Arms)
	return c, len(c.Daily), err
}

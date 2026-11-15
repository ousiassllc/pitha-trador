// Package promotiondiag is the read-only analysis behind the Paper results MCP's
// promotion_checklist tool (issue #1091): PROPOSAL EVIDENCE for a person to read before
// a candidate (another prompt version, trial arm, research model, ...) is proposed to
// replace the current Jev answer. It lists eleven fixed checks with their numbers and
// says which is the first not met. It applies nothing: no setting, no prompt version,
// no model is changed, nothing is written, and `eligible_as_evidence` is not an
// instruction. Adoption is decided by a person (after Airi's proposal and Yuta's OK).
//
// The numeric criteria are documented in docs/architecture/research-tools/paper-results-mcp-promotion.md;
// each documented value is the floor of promotionsettings and a setting or a call can
// only make it stricter. The numbers come from ledgerdiag.Compute, so the checklist and
// the candidate ledger never disagree.
package promotiondiag

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-promotion.md).
const Description = "候補（シャドーのプロンプト版・お試しのアーム・研究用モデル・ベースライン・別コホート）を、いまのもの（既定はライブの Jev）に置き換える提案の「証拠」を、" +
	"固定の 11 項目で並べる読み取り専用のチェックリスト。何も適用せず（applied は常に false）、設定・プロンプト版・モデルを変えず、何も書き込まない。" +
	"eligible_as_evidence（全項目 pass）は指示ではなく、採用は人間（あおいの提案と悠太さんの OK）が決める。" +
	"数字は candidate_ledger と同じ計算（同じ行・同じコスト控除後ラベル・日クラスタの区間・Holm 補正）。" +
	"項目: same_rows_and_period・common_rows・common_days・diff_ci_lower・multiple_comparison・positive_days・not_one_day_wins・max_drawdown・after_ai_cost・after_training_cutoff・model_version_stable。" +
	"各項目は status（pass / fail / unknown）・actual・required・detail、全体は first_unmet（最初に満たさなかった項目）。" +
	"しきい値は promotionsettings のフロア（docs/architecture/research-tools/paper-results-mcp-promotion.md）か、Settings の research.promotion.* で厳しくした値で、呼び出しの min_* / max_adjusted_p は厳しくする方向にだけ指定できる（緩めると invalid_argument）。" +
	"設定の組（experiment・setting）は同じ行で比べられないので unsupported_candidate_kind。usd_jpy と notional_jpy が両方無ければ after_ai_cost は unknown"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	ref := func(desc string, kinds []string) map[string]any {
		return map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"kind", "id"},
			"properties": map[string]any{
				"kind": map[string]any{"type": "string", "enum": kinds},
				"id": map[string]any{"type": "string", "minLength": 1, "maxLength": ledgerdiag.MaxIDLen,
					"description": "shadow=プロンプト版 / trial=アーム id / baseline=random・momentum・contrarian・none・long_leg・short_leg / research_model=<algorithm>:<30m|60m|close> / live_cohort=<model_id>/<version>"},
			},
			"description": desc,
		}
	}
	return map[string]any{
		"candidate":             ref("提案する候補", candidateKinds),
		"current":               ref("置き換え元。省略はライブの Jev（kind=live の id は live）", append([]string{ledgerdiag.KindLive}, candidateKinds[:4]...)),
		"horizon":               map[string]any{"type": "string", "enum": []string{ledgerdiag.Horizon10m, ledgerdiag.Horizon30m, ledgerdiag.Horizon60m, ledgerdiag.HorizonClose}, "default": ledgerdiag.Horizon10m, "description": "ラベルのホライズン（candidate_ledger と同じ）"},
		"usd_jpy":               map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "USD/JPY。notional_jpy と両方渡したときだけ after_ai_cost を判定する"},
		"notional_jpy":          map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "1 判断あたりの想定元本（円）。usd_jpy と両方渡したときだけ after_ai_cost を判定する"},
		"family_size_extra":     map[string]any{"type": "integer", "minimum": 0, "maximum": MaxFamilyExtra, "default": 0, "description": "他の場所で試した候補の数。Holm の族に足して補正を強める（p=1 の比較として）"},
		"min_common_rows":       map[string]any{"type": "integer", "minimum": 1, "description": "共通の行数の最小。厳しくする方向にだけ（フロア 300 か Settings の値以上）"},
		"min_days":              map[string]any{"type": "integer", "minimum": 1, "description": "共通の行が及ぶ JST 日数の最小。厳しくする方向にだけ（フロア 10 か Settings の値以上）"},
		"min_diff_ci_lower_bps": map[string]any{"type": "number", "description": "差（候補−いま）の日クラスタ区間の下端の最小 bps。厳しくする方向にだけ（フロア 0 か Settings の値以上）"},
		"min_positive_day_pct":  map[string]any{"type": "number", "minimum": 0, "maximum": 100, "description": "候補が勝った日の割合（%）の最小。厳しくする方向にだけ（フロア 50 か Settings の値以上）"},
		"max_adjusted_p":        map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 1, "description": "Holm 補正後の p の最大。厳しく（小さく）する方向にだけ（フロア 0.05 か Settings の値以下）"},
	}
}

// Run is the tool: the checklist of the candidate over [r.From, r.To) and the number of
// common rows it is computed on. The clock is not used (the signature matches the other analyses).
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	out, err := Check(ctx, q, r, req)
	if err != nil {
		return nil, 0, err
	}
	return out, out.KeyNumbers.CommonRows, nil
}

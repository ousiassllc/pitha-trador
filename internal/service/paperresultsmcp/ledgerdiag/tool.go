// Package ledgerdiag is the read-only analysis behind the Paper results MCP's
// candidate_ledger tool (issue #1090): the candidates that could replace or
// complement the live Jev answer (another model's cohort, a shadow prompt version, a
// trial arm, the research model, the simple baselines) lined up on the SAME period
// and the SAME decision rows, with the same cost-inclusive label, day-cluster
// intervals, the differences to the live reference and multiple-comparison-corrected
// p-values.
//
// The ledger decides nothing: no winner, no ranking, no vote, no recommendation, no
// threshold. It only reads (every statement is a SELECT) and nothing it computes feeds
// Jev, Risk, Policy, an order or the meta-model. Compute is the typed computation the
// promotion checklist (promotiondiag) reuses.
package ledgerdiag

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-candidate-ledger.md).
const Description = "ライブの Jev（AI）と、置き換え・併用の候補（別モデルのコホート・シャドーのプロンプト版・お試しのアーム・研究用モデル・単純なベースライン）を、" +
	"同じ期間・同じ判断の行・同じコスト控除後ラベルで並べる読み取り専用の台帳（研究用。勝者・順位・多数決・推奨・しきい値は無い。何も決めない）。" +
	"基準（reference）は期間内のライブの版の trader LONG/SHORT 判断の llm アーム。候補の 1 行の net bps は、その候補の方向のラベル（LONG は買い・SHORT は売りの net、NONE・取引なしは 0）。" +
	"候補ごとに、答えた件数・共通の行数・欠測率（失敗・拒否・答えなしは欠測に数える）、共通の行での平均 net bps（日クラスタのブートストラップ 95% 区間・日数）、" +
	"ライブとの差（候補−ライブを同じ行ごとに。区間・検出できる最小の差・p 値・Holm 補正後の p と比較の族の大きさ）、AI の費用（USD。reported・推定・不明を分け、不明を 0 円にしない）、" +
	"usd_jpy と notional_jpy を両方渡したときだけ費用の bps 換算と費用控除後の平均、候補ごとの model_id の種類を返す。" +
	"horizon は 10m（既定。既存の triple barrier ラベル）・30m・60m・close（research_horizon_labels。両側のラベルが無い判断は全員から除外して件数を出す）。" +
	"alignment は common_all（既定。基準と全候補が答えた行だけ）か pairwise（各候補を、ライブと共通の行だけで比べる）。" +
	"応答本文・エラー本文・API キーは返さず、何も書き込まない"

// The values of the horizon and alignment arguments.
const (
	Horizon10m   = "10m"
	Horizon30m   = "30m"
	Horizon60m   = "60m"
	HorizonClose = "close"

	AlignCommonAll = "common_all"
	AlignPairwise  = "pairwise"
)

// Limits of one call. A request beyond them is query.ErrLimitExceeded (or an invalid
// argument when the caller listed the candidates) rather than a partial result.
const (
	// MaxCandidates is the most candidates one call lines up.
	MaxCandidates = 60
	// MaxPredictionRows is the most research_model_predictions rows one call reads.
	MaxPredictionRows = 200_000
	// MaxIDLen is the longest candidate id.
	MaxIDLen = 160
)

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"horizon": map[string]any{
			"type": "string", "enum": []string{Horizon10m, Horizon30m, Horizon60m, HorizonClose}, "default": Horizon10m,
			"description": "ラベルのホライズン。10m=既存の triple barrier（10 分）、30m・60m・close=研究用ホライズンラベル（research_horizon_labels）",
		},
		"alignment": map[string]any{
			"type": "string", "enum": []string{AlignCommonAll, AlignPairwise}, "default": AlignCommonAll,
			"description": "common_all=基準と全候補が答えた行だけで全員を比べる / pairwise=各候補を、ライブと共通の行だけで比べる",
		},
		"candidates": map[string]any{
			"type": "array", "maxItems": MaxCandidates, "uniqueItems": true,
			"items": map[string]any{"type": "string", "minLength": 1, "maxLength": MaxIDLen},
			"description": "候補の id。live_cohort:<model_id>/<version>・shadow:<prompt_version>・trial:<arm_id>・baseline:<random|momentum|contrarian|none|long_leg|short_leg>・" +
				"research_model:<algorithm>:<30m|60m|close>。省略は期間内に見つかった全候補",
		},
		"usd_jpy":      map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "USD/JPY。notional_jpy と両方渡したときだけ AI の費用を bps に換算する（既定値は置かない）"},
		"notional_jpy": map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "1 判断あたりの想定元本（円）。usd_jpy と両方渡したときだけ費用を bps に換算する（既定値は置かない）"},
		"model_id":     map[string]any{"type": "string", "maxLength": evaldiag.MaxModelIDLen, "description": "基準（ライブ）をこの model_id の判断だけにする。省略はライブの版の全モデル（複数あれば reference.multiple_model_ids を立てる）"},
	}
}

// Run is the tool: the ledger of [r.From, r.To) and the number of reference rows.
// The clock is not used (the signature matches the other analyses).
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	l, err := Compute(ctx, q, r, req)
	if err != nil {
		return nil, 0, err
	}
	return l, l.Reference.Rows, nil
}

package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

// metalabel_training_readiness (issue #848; docs/api/endpoints/
// paper-results-mcp.md §3.23): whether the labeled Jev decisions can feed the
// meta-labeling retraining. It reads through the read-only connection and
// writes nothing.

func init() {
	tools = append(tools, metalabelReadinessTool)
}

var metalabelReadinessTool = tool{
	name: "metalabel_training_readiness",
	description: "メタモデルの学習データがそろっているか: 学習の条件ごとに絞り込んだあとの行数（trader→LONG/SHORT→ラベル→net_return_bps→8特徴量）、" +
		"学習はライブのプロンプトと同じコホートの行だけ（別コホートの版は excluded_prompt_versions に出る。昇格後は新しい版の行から貯め直す）。特徴量ごとの欠け件数と dropped_incomplete、直近の再学習の見送り（見送りが記録した dropped_incomplete。記録が無ければ null と理由コード）、" +
		"いまの metalabel.llm_cutoff で分けた学習側（fit+較正）と OOS 側の行数・必要件数・見送りコード、OOS 窓の始点を決めているもの（oos_start_reason）・学習できるまでの不足行数と営業日の見積もり（split.shortfall）・除外した別コホートの版ごとの件数と判断日（prompt_cohort）。必要件数は再学習の設定（walk-forward と採用条件の既定）から取る（読み取り専用）",
	schema: periodSchema(nil),
	decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		var a periodArgs
		if err := strictDecode(raw, &a); err != nil {
			return a, nil, err
		}
		return a, func(ctx context.Context, e toolEnv) (any, int, error) {
			live, err := jev.ResolveLiveVersion(ctx, shadowquery.SettingsStore(e.q))
			if err != nil {
				return nil, 0, err
			}
			d, err := metalabeldiag.TrainingReadinessAt(ctx, e.q, e.r, e.now, live, jev.ForeignVersions(live))
			return d, len(d.Funnel), err
		}, nil
	},
}

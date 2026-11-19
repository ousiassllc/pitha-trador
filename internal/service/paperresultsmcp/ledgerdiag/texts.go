package ledgerdiag

import (
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

const (
	notice = "研究用の読み取り専用の台帳です。勝者・順位・多数決・推奨・しきい値は無く、何も決めません。数字は同じ期間・同じ判断の行・同じコスト控除後ラベルでの記述で、採用・昇格の基準ではありません。" +
		"Jev・Risk・Policy・注文・メタモデルの学習には使われず、何も書き込みません"
	statisticsText = "日クラスタのブートストラップ（判断の JST 日を単位に復元抽出）の百分位区間。検出できる最小の差 = (z(1-alpha/2) + z(power)) × ブートストラップの標準誤差。p 値は平均/標準誤差の正規近似の両側"
	correctionText = "Holm-Bonferroni（差の p 値を持つ全候補が 1 つの族。family_size_extra がある呼び出しでは、他で試した比較を p=1 として族に足す＝補正を強める方向にだけ動かす）"
)

func statistics() Statistics {
	return Statistics{
		Method: statisticsText, Alpha: evaldiag.Alpha, Power: evaldiag.Power, Resamples: evaldiag.Resamples, Seed: evaldiag.Seed,
		Correction: correctionText,
	}
}

func definitions() map[string]string {
	return map[string]string{
		"reference":          "ライブの版（jev.ResolveLiveVersion）の期間内の trader LONG/SHORT 判断の llm アーム（その判断自身の方向のラベル）。model_id で絞れる。複数の model_id が混ざる期間は multiple_model_ids=true",
		"row_net":            "候補のある行の net bps = その候補の方向のラベル（LONG=買いの net、SHORT=売りの net、NONE・取引なし=0）。全候補が同じコスト控除後ラベル（10m は既存の triple barrier、30m・60m・close は research_horizon_labels）",
		"common_all":         "基準と、答えのある全候補が答えた行だけで全員を比べる。答えが 1 件も無い候補は no_answers=true で、積集合から外す（他の候補の表を空にしないため）",
		"pairwise":           "各候補を、ライブと共通の行（両方が答えた行）だけで比べる。候補ごとに行が違う",
		"missing_rate":       "1 - n_common / 基準の行数。候補が答えなかった行（記録なし・trial の failed / refused / partial）は欠測。common_all では他の候補が欠測の行も共通から外れるので outside_common に数える",
		"diff_vs_live":       "同じ行ごとの（候補の net − ライブの net）。日ごとの差の合計を日クラスタとして区間を作る。live_cohort は別の判断を見ているので null（different_decisions）",
		"holm":               "差の p 値を持つ全候補を 1 つの族とする Holm 補正後の p。family_size は族の大きさ",
		"ai_cost":            "候補の AI の費用（USD）。net bps とは別に出す。reported=報告値、list_price_estimate・usage_estimate=推定、unknown=不明（金額なし。0 円にしない）、recorded=ライブの request_cost、none=有料 API なし",
		"combined":           "usd_jpy と notional_jpy を両方渡したときだけ: cost_bps_per_row = usd_per_row × usd_jpy / notional_jpy × 10000、mean_net_after_ai_cost_bps = 平均 net − cost_bps_per_row、diff_after_ai_cost_bps = 差の平均 −（候補の cost_bps − ライブの cost_bps）。費用に不明の行があれば出さない",
		"baseline":           "random・momentum・contrarian・none・long_leg・short_leg は jev_evaluation と同じ定義。費用 0",
		"research_model":     "research_model_predictions の予測方向（LONG/SHORT はその側のラベル、NONE は 0）。algorithm と label_horizon ごとに 1 候補。同じ判断に複数の run の予測があれば最新の run",
		"distinct_model_ids": "期間内に答えたモデルの種類（live=jev_decisions.model_id、shadow=model_id、trial=served_model）。複数あればモデルが途中で変わっている",
	}
}

func caveats(cutoff evaldiag.Cutoff) []string {
	out := []string{
		"件数が同じ日・同じ銘柄に偏るため、日クラスタの区間でも楽観的になりうる。日数が少ないと区間は出ない",
		"比べる候補が多いほど偶然に良く見える候補が出る。Holm 補正後の p と family_size を見る。他の場所で試した候補は promotion_checklist の family_size_extra で族に足せる",
		"shadow・trial の答えはライブの判断と同じ入力に対するもので、同じ判断の行が基準。trial は入力の切り詰めやモデル変更があり得るので distinct_model_ids を見る",
		"horizon が 30m・60m・close の row_net は、固定ホライズンで決済した research_horizon_labels（TP/SL バリアなし）",
	}
	if !cutoff.Applied {
		out = append(out, "metalabel.llm_cutoff が仮置き（"+metalabelsettings.DefaultLLMCutoff+"）のため、学習データに含まれうる日も除外していない（llm_cutoff.applied=false）")
	}
	return out
}

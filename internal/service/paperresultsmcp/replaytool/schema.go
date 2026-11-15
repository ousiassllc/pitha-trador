package replaytool

import (
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	domainreplay "github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
)

// The tools' descriptions (docs/api/endpoints/paper-results-mcp-jev-replay.md).
const (
	EstimateDescription = "リプレイ評価（保存済みのライブの trader 判断の入力を、お試しのアームに後から送って比べる）の開始前の見積もり（読み取り専用）: " +
		"期間（ライブの判断の時刻。省略は直近 30 日）・max_rows（必須。対象の判断の上限。古い順）・アーム（省略は jev.trial.arms の全アーム）を指定すると、" +
		"判断の件数 × アーム数の呼び出し数、アームごとの学習カットオフの扱い（llm_cutoff が公表済みならその日より後の判断だけを聞き、除外した件数を excluded_before_cutoff に出す。" +
		"未公表は llm_cutoff_status=unverified＝未確認で、結果は参考扱い）、費用の見込み（max_cost_usd＝予約と同じ最悪値側の単価。list_price_usd＝定価の目安）と、" +
		"開始に必要な confirm_token を返す。何も送らず、何も書かない。API キーは返さない"
	StartDescription = "リプレイ評価の run を 1 件開始する（待ち状態で記録するだけ。呼び出しはアプリ内のランナーが行い、jev.replay.enabled がオンで、API キーと上限の範囲内のときだけ進む）。" +
		"replay_estimate と同じ引数と、その confirm_token が必要（見積もり以後に内容が変わると拒否。見積もりを経ない開始はできない）。" +
		"スイッチ jev.replay.enabled がオフなら開始できない（オンにできるのは Settings 画面だけで、この MCP からは変えられない）。同時に動かせる run は 1 件。" +
		"書き込み許可がオフのときは write_disabled。変更するのは jev_replay_runs への 1 行の追加だけで、取引の設定・オン/オフのスイッチには触らない"
	StopDescription = "実行中・待ち状態のリプレイの run を止める（停止フラグを立てるだけ。ランナーは次の呼び出しの前に止まり、記録済みの結果は残る）。" +
		"終わった run は not_active。変更するのは jev_replay_runs の停止フラグだけ。費用を減らす操作なので書き込み許可のスイッチには左右されない"
	ProgressDescription = "リプレイの run の状態と進み具合（読み取り専用）: run_id を指定すればその run、省略すれば新しい順の一覧。" +
		"status（pending / running / completed / stopped / failed）・停止フラグ・止まった理由 stop_reason（requested / run_cost_cap / payment_required / key_not_set / no_arms）・" +
		"待っている理由 blocked_reason（日次上限・停止中・待機など。日が変われば自動で続きから再開）・見積もり・呼び出し数（計画・記録済み・失敗・残り）・数えた費用。キー・応答本文・エラー本文は返さない"
	ResultsDescription = "リプレイの run の結果（読み取り専用）: jev_trial_compare と同じ指標（cohort・共通の成功数 common_success_count・欠測率 missing_rate・ラベルでの当たり率と live_baseline・" +
		"見送りの反実仮想ラベル proxy）を、この run の行とライブの判断を同じ行どうしで比べて返し、アームごとの行数・参考扱い（reference_only）の行数・学習カットオフの確認状態（未確認など）・費用を添える。" +
		"unverified（未確認）のアームの結果は参考扱い。ライブ・取引・学習・RAG には使われない。応答本文・エラー本文・キーは返さない"
)

// PlanProperties are the arguments of replay_estimate and replay_start besides
// the period.
func PlanProperties() map[string]any {
	return map[string]any{
		"max_rows": map[string]any{
			"type": "integer", "minimum": 1, "maximum": domainreplay.MaxRows,
			"description": "対象にするライブの判断の件数の上限（必須。古い順に数える）。1 つの run の上限を呼び出し側が決める",
		},
		"arms": map[string]any{
			"type": "array", "maxItems": jevtrialsettings.MaxArms, "uniqueItems": true,
			"items":       map[string]any{"type": "string", "minLength": 1, "maxLength": 48},
			"description": "対象のアーム id（jev.trial.arms の id）。省略は設定されている全アーム",
		},
		"decision_type": map[string]any{
			"type": "string", "enum": []string{domainreplay.DecisionTypeTrader}, "default": domainreplay.DecisionTypeTrader,
			"description": "判断の種類。trader だけ（アームは trader の質問群に答える）",
		},
	}
}

// StartProperties are replay_start's arguments besides the period: the plan's
// and the token of its estimate.
func StartProperties() map[string]any {
	props := PlanProperties()
	props["confirm_token"] = map[string]any{
		"type": "string", "minLength": 1, "maxLength": 64,
		"description": "replay_estimate が返した confirm_token（同じ期間・max_rows・アームで取った直近の見積もりのもの）",
	}
	return props
}

// RunIDProperty is the run_id argument.
func RunIDProperty() map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "description": "リプレイの run の id（replay_start・replay_progress が返す run_id）"}
}

// ArmsProperty is the optional arms filter of replay_results.
func ArmsProperty() map[string]any {
	return map[string]any{
		"type": "array", "maxItems": jevtrialsettings.MaxArms, "uniqueItems": true,
		"items":       map[string]any{"type": "string", "minLength": 1, "maxLength": 48},
		"description": "比べるアーム id（省略は run の全アーム）",
	}
}

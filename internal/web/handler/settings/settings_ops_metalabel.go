package settings

import "github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"

// metaLabelGroup holds the meta-labeling secondary model's settings (issue
// #759, parent #752): which model and calibration it uses, when it is
// retrained at night and the minimum expected value for a take.
var metaLabelGroup = opsGroup{
	id:          "metalabel",
	name:        "メタラベリング（入る/見送るの二次モデル）",
	description: "Jev の方向判断（一次シグナル）に対し、実際に入るか見送るかとサイズを決める二次モデルの設定です（issue #752）。較正後の期待値（EV、bps）が 0 を超え、かつ下記の最低 EV を超えたときだけ「入る」になります。有効なモデルが無い・特徴量が欠けているときは見送ります。検証は paper のみです。",
	note: "保存した値は再起動不要で、次の学習・判定から反映されます（学習済みのモデルは変わりません）。環境変数では設定できません。" +
		"保存済みの値が不正になっていた場合は、全項目とも既定値を使い、ログに警告を出します。",
	fields: []opsField{
		{key: metalabelsettings.KeyModel, label: "モデル", options: metalabelsettings.Models,
			hint: "二次モデルのアルゴリズムです。現在選べるのは logistic_regression（ロジスティック回帰）だけです。既定は logistic_regression です。"},
		{key: metalabelsettings.KeyCalibration, label: "較正方法", options: metalabelsettings.Calibrations,
			hint: "モデルの勝率を実際の勝率に合わせる方法です。platt は標本が少なくても安定し、isotonic は標本が多いときに向きます。既定は platt です。"},
		{key: metalabelsettings.KeyRetrainTime, label: "夜間の再学習時刻（JST）", placeholder: "18:00",
			hint: "HH:MM 形式です。立会時間とラベル確定前の " + metalabelsettings.RetrainBlockedFrom + "〜" + metalabelsettings.RetrainBlockedTo + " は指定できません。既定は 18:00 です。"},
		{key: metalabelsettings.KeyMinEVBps, label: "最低 EV（bps）", placeholder: "0",
			hint: "較正後の期待値（コスト控除後）がこの値を超えたときだけ「入る」にします。0 以上 1000 以下で、0 でも EV が 0 以下なら見送ります。既定は 0 bps です。"},
		{key: metalabelsettings.KeyLLMCutoff, label: "Jev モデルの学習データの終わり（YYYY-MM-DD）", placeholder: metalabelsettings.DefaultLLMCutoff,
			hint: "Jev（LLM）が学習し終えた日付です。夜間再学習の検証（OOS）は、この日以降の判断だけで行います（LLM が結果を既に知っている期間で評価しない：look-ahead bias 対策）。使っている Jev のモデルの実際の日付に合わせてください。既定は " + metalabelsettings.DefaultLLMCutoff + " です。"},
	},
}

package settings

import (
	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

// researchJobsGroup holds the switches and retentions of the research-only daily jobs of
// issues #1089 (daily Jev evaluation record) and #1092 (research model).
var researchJobsGroup = opsGroup{id: "research-jobs", name: "研究用の日次評価の記録と学習",
	description: "Jev の評価の主な数字を毎日 1 行ずつ研究専用の表に残す記録と、特徴量だけで学習する研究用モデル（AI を使わない）の夜間の学習です。どちらも取引の判断・Risk・発注・Paper ガード・Live・Jev・メタモデルには一切使いません。",
	note: "どちらも既定はオフ（無効）で、オフの間は日次タスクが何も書きません。オンにすると 16:00 JST 以降のメンテナンスチェック（10 分ごと）から、それぞれ今日までの未記録の日を 1 日ずつ処理します。" +
		"保持期間より古い行は、それぞれの研究用の表だけから削除されます。保存した値は再起動不要で、次のチェックから反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: researchjobsettings.KeyEvalEnabled, label: "評価の日次記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		{key: researchjobsettings.KeyEvalRetentionDays, label: "評価の記録の保持期間（日）", placeholder: "365",
			hint: "1〜3650 の整数です。この日数（JST の日）より古い行を評価の日次記録の表から削除します。既定は 365 日です。"},
		{key: researchjobsettings.KeyModelEnabled, label: "研究用モデルの夜間の学習", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。予測は研究専用の表に残すだけで、取引には使いません。"},
		{key: researchjobsettings.KeyModelRetentionDays, label: "研究用モデルの記録の保持期間（日）", placeholder: "90",
			hint: "1〜3650 の整数です。この日数（JST の日）より古い学習の記録と予測を研究用の表から削除します。既定は 90 日です。"},
	}}

// promotionGroup holds the criteria of the read-only promotion checklist (issue #1091).
var promotionGroup = opsGroup{id: "research-promotion", name: "昇格チェックリストの基準",
	description: "Paper 実績 MCP の promotion_checklist（候補を今のものと同じ条件で比べる読み取り専用のツール）が当てる数字の基準です。このツールは何も適用しません（設定・プロンプト・モデルは変わりません）。",
	note: "基準は、仕様書（docs/architecture/paper-results-mcp-promotion.md）に書いた値より厳しい方向にだけ変えられます（最小値は上げる方向、p 値の上限は下げる方向）。緩める値は保存できません。" +
		"保存した値は再起動不要で、次の呼び出しから反映されます。",
	fields: []opsField{
		{key: promotionsettings.KeyMinCommonRows, label: "共通の行数の下限", placeholder: promotionsettings.DefaultText(promotionsettings.KeyMinCommonRows),
			hint: "候補と今のものが共有する判断の行数の下限です。300 以上の整数。"},
		{key: promotionsettings.KeyMinDays, label: "日数の下限", placeholder: promotionsettings.DefaultText(promotionsettings.KeyMinDays),
			hint: "その行がまたぐ JST の日数の下限です。10 以上の整数。"},
		{key: promotionsettings.KeyMinDiffCILowerBps, label: "差の信頼区間の下限（bps）", placeholder: promotionsettings.DefaultText(promotionsettings.KeyMinDiffCILowerBps),
			hint: "コスト込みの差（候補 − 今）の日クラスタ区間の下限がこの値を超えること。0 以上。"},
		{key: promotionsettings.KeyMinPositiveDayPct, label: "候補が勝った日の割合の下限（%）", placeholder: promotionsettings.DefaultText(promotionsettings.KeyMinPositiveDayPct),
			hint: "日ごとに見て候補が今のものを上回った日の割合の下限です。50 以上 100 以下。"},
		{key: promotionsettings.KeyMaxAdjustedP, label: "補正後の p 値の上限", placeholder: promotionsettings.DefaultText(promotionsettings.KeyMaxAdjustedP),
			hint: "多重比較（Holm）の補正後の p 値の上限です。0.05 以下（小さいほど厳しい）。"},
	}}

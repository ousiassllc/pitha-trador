package settings

import "github.com/ousiassllc/pitha-trador/internal/config/researchlabelsettings"

// researchLabelGroup holds the switch and the retention of the research-only
// horizon labels (issues #1073, #1074).
var researchLabelGroup = opsGroup{id: "research-labels", name: "研究用ホライズンラベルの記録",
	description: "全スナップショットについて、30 分・60 分・引けまで保有した場合のコスト前後の bps を、研究専用の表に毎日記録します。取引の判断・Risk・発注・Paper ガード・Live・Jev には一切使いません。",
	note: "既定はオフ（無効）で、オフの間は日次タスクが何も書きません（過去に記録した行も、オフの間は削除されません）。オンにすると、16:00 JST 以降のメンテナンスチェック（10 分ごと）から、今日より前の未記録の日を古い順に 1 日ずつ記録します（初回は残っている全期間を数回に分けて追いつきます）。" +
		"記録の合間には他のメンテナンス（バックアップなど）が先に動けます。保持期間より古い行は、研究用のラベルの表だけから削除されます（日ごとの集計は残ります）。" +
		"オン・オフは Paper 実績 MCP からも変更できます（保持期間は変更できません）。保存した値は再起動不要で、次のチェックから反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: researchlabelsettings.KeyEnabled, label: "研究用ラベルの記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		{key: researchlabelsettings.KeyRetentionDays, label: "保持期間（日）", placeholder: "90",
			hint: "1〜3650 の整数です。この日数（JST の日）より古い行を研究用のラベルの表から削除します。既定は 90 日です。"},
	}}

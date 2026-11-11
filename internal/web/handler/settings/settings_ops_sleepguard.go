package settings

import "github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"

// sleepGuardGroup holds the sleep guard switch.
var sleepGuardGroup = opsGroup{id: "sleep-guard", name: "スリープ防止（取引日の立会時間帯）",
	description: "取引日の 08:45 から大引け（15:30）までの間、PC が自動でスリープしないようにします。スリープすると市況データの収集が止まり、その日のデータは後から取り戻せないためです。立会日の欠け（終日・日中）を防ぐための選択肢で、欠けた日は Home の要対応に出ます。",
	note:        "既定はオフ（無効）です。オンにすると、取引日の 08:45〜大引けの間だけ OS に「システムを起こしたままにする」よう要求し、時間外・非取引日・アプリ終了時は OS の電源設定に戻ります（画面の消灯は妨げません）。現在は Windows のみ対応で、他の OS では保存しても何も起きません。ノート PC の蓋を閉じたときの動作や、手動のスリープ・シャットダウンは防げません。保存した値は再起動不要で、次の判定（30 秒以内）から反映されます。Paper 実績 MCP からは変更できません。環境変数では設定できません。",
	fields:      []opsField{{key: sleepguardsettings.Key, label: "スリープ防止", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"}}}

package settings

import "github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"

// jquantsGroup holds the switch, the plan flags, the daily request cap, the
// backfill window and the deletion request of the J-Quants read adapter.
var jquantsGroup = opsGroup{id: "jquants-fetch", name: "J-Quants（日足・分足の取り込み）",
	description: "契約した J-Quants から、日足・分足・銘柄一覧を研究専用の表へ取り込みます。取引の判断・Risk・発注・Paper ガード・Live・Jev には一切使いません。個人の私的利用の範囲で使い、外部へ送りません。",
	note: "既定はオフ（無効）で、オフの間は外部への要求を 1 回も送りません。API キーは「外部連携・AI」ページの J-Quants で入れます（キーが未設定のあいだも送りません）。" +
		"オンにする前に、契約したデータ（日足・分足）に合わせてプランの欄をオンにしてください。契約していない側は、オンにしても呼びません。" +
		"オンにすると、16:00 JST 以降のメンテナンスチェックから、昨日までの取りこぼした日を古い順に少しずつ取り込みます（1 日の要求数の上限に達したら、その日はそこで止まり、翌日に続きます）。" +
		"分足を取り込んだ日は、その日の研究用ホライズンラベルのうち、記録の足が欠けて付かなかった窓だけを、取り込んだ分足の出口で付け直します（付け直した行は由来が J-Quants と記録されます）。" +
		"解約したときは、下の「取り込んだデータの削除」をオンにして保存してください。次のメンテナンスチェックで、J-Quants 由来の行だけ（取り込んだ生データ・取得済みの印・J-Quants の出口で付けたラベル）を削除し、この項目は自動でオフに戻ります。ほかの出どころのデータには触れません。" +
		"これらの設定は Paper 実績 MCP からは変更できません。保存した値は再起動不要で、次のチェックから反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: jquantssettings.KeyEnabled, label: "J-Quants の取り込み", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		{key: jquantssettings.KeyPlanDaily, label: "契約: 日足", onOff: true, hint: "日足を契約しているときだけオンにします。既定はオフです。"},
		{key: jquantssettings.KeyPlanMinute, label: "契約: 分足", onOff: true, hint: "分足（アドオン）を契約しているときだけオンにします。既定はオフです。"},
		{key: jquantssettings.KeyDailyRequestCap, label: "1 日の要求数の上限", placeholder: "200",
			hint: "1〜5000 の整数です。JST の 1 日に J-Quants へ送る要求（ページ送りを含む）の上限で、超える要求は送りません。既定は 200 です。"},
		{key: jquantssettings.KeyBackfillDays, label: "埋め戻す日数", placeholder: "30",
			hint: "1〜1830 の整数です。昨日からさかのぼって、取りこぼした日を探す日数です。分足は契約上、過去 2 年までです。既定は 30 日です。"},
		{key: jquantssettings.KeyPurgeRequested, label: "取り込んだデータの削除", onOff: true,
			hint: "解約したときにオンにして保存します。J-Quants 由来のデータだけを削除し、終わるとオフに戻ります。オフの間でも実行されます。"},
	}}

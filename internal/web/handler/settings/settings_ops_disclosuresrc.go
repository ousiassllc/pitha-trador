package settings

import "github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"

// disclosureSrcGroup holds the switches, the daily request caps, the EDINET
// lookback and the retention of the extra disclosure / news sources (issue #1096).
var disclosureSrcGroup = opsGroup{id: "disclosure-src", name: "開示・ニュースの追加の情報源（EDINET・立花）",
	description: "EDINET の書類一覧（提出日時・書類の種類・銘柄コード・提出者名・表題のメタデータだけ。書類の本文は取得しません）と、立花証券のニュース（見出し・時刻・関連銘柄）を、研究専用の表へ記録します。" +
		"取引の判断・ゲート・Policy・Risk・発注・Paper ガード・Live・Jev・既存の開示見送りルールには一切使いません。",
	note: "既定はどちらもオフ（無効）で、オフの間は外部への要求を 1 回も送りません。EDINET の API キーは「外部連携・AI」ページの EDINET で入れます（キーが未設定のあいだも送りません）。" +
		"EDINET をオンにすると、18:00 JST 以降のメンテナンスチェックから、直近の営業日の書類一覧を新しい日から順に少しずつ取り込みます（1 日の要求数の上限に達したら、その日はそこで止まり、翌日に続きます。取った日が終わったあとにもう一度取り直します）。" +
		"立花のニュースは、立花証券の実機での計測が終わるまで、この版では本番に繋がっていません。オンにしても何も取得せず、記録は 0 件のままです。" +
		"これらの設定は Paper 実績 MCP からは変更できません（MCP からは件数・最終取得時刻・失敗件数を読めるだけです）。保存した値は再起動不要で、次のチェックから反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: disclosuresrcsettings.KeyEdinetEnabled, label: "EDINET の書類一覧の記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		{key: disclosuresrcsettings.KeyEdinetDailyRequestCap, label: "EDINET: 1 日の要求数の上限", placeholder: "30",
			hint: "1〜1000 の整数です。JST の 1 日に EDINET へ送る要求の上限で、超える要求は送りません（失敗した要求も数えます）。既定は 30 です。"},
		{key: disclosuresrcsettings.KeyEdinetLookbackDays, label: "EDINET: さかのぼる日数", placeholder: "7",
			hint: "1〜3650 の整数です。今日からさかのぼって書類一覧を取る日数です（保持期間より長くは取りません）。既定は 7 日です。"},
		{key: disclosuresrcsettings.KeyTachibanaNewsEnabled, label: "立花ニュースの記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。実機計測が終わるまで、オンにしても何も取得しません。"},
		{key: disclosuresrcsettings.KeyTachibanaNewsDailyRequestCap, label: "立花ニュース: 1 日の要求数の上限", placeholder: "24",
			hint: "1〜1000 の整数です。JST の 1 日にニュースの取得へ送る要求の上限です。既定は 24 です。"},
		{key: disclosuresrcsettings.KeyRetentionDays, label: "保持期間（日）", placeholder: "1095",
			hint: "30〜3650 の整数です。この日数より古い行を、この 3 つの記録の表から削除します。既定は 1095 日です。"},
	}}

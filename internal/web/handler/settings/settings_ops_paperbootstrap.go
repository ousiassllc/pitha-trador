package settings

import "github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"

// paperBootstrapGroup holds the Paper-only bootstrap entry switch.
var paperBootstrapGroup = opsGroup{
	id:   "paper-bootstrap",
	name: "Paper ブートストラップ（メタモデル未採用時のエントリー）",
	description: "採用済みのメタモデルが無い間は、全候補がリスクエンジンに届く前に見送られ、Paper の取引が 0 件のままラベルが貯まりません。" +
		"有効にすると、メタモデルが採用されるまでの間だけ、Paper で従来のルール（Jev の確信度が Policy Engine の最低確率以上など）でエントリーを判断し、ラベルを貯めます。",
	note: "既定は 0（無効）で、Paper でのみ有効にできます。Live では保存値があっても必ず無効で、Live では 1 を保存できません。" +
		"新しいしきい値は作らず、Policy Engine（ロング／ショート）の既存のしきい値と、確信度帯ごとのキャリブレーション件数をそのまま使い、リスクエンジンの判定は変わりません。" +
		"メタモデルが採用された時点で自動的に使われなくなります。保存した値は再起動不要で、次の評価から反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: paperbootstrapsettings.Key, label: "ブートストラップ（Paper 限定）", placeholder: "0（無効）",
			hint: "1 で有効、0 で無効です。既定は 0（無効）です。Paper 以外では 1 を保存できません。"},
	},
}

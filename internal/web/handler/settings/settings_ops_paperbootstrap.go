package settings

import "github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"

// paperBootstrapGroup holds the Paper-only bootstrap entry switch (issue #798).
var paperBootstrapGroup = opsGroup{
	id:   "paper-bootstrap",
	name: "Paper ブートストラップ（メタモデル未採用時のエントリー）",
	description: "採用済みのメタモデルが無い間は、全候補がリスクエンジンに届く前に見送られ（meta_skip: no_active_model）、Paper の取引が 0 件のままラベルが貯まりません。" +
		"有効にすると、メタモデルが採用されるまでの間だけ、Paper で旧ルール（Jev の確信度が policy.*.min_probability 以上など）でエントリーを判断し、ラベルを貯めます。",
	note: "既定は 0（無効）で、Paper でのみ有効にできます。Live では保存値があっても必ず無効で、Live では 1 を保存できません（Paper 限定、issue #798）。" +
		"新しいしきい値は作らず、Policy Engine（ロング／ショート）の既存のしきい値と、確信度帯ごとのキャリブレーション件数（not_calibrated）をそのまま使い、リスクエンジンの判定は変わりません。" +
		"メタモデルが採用された時点で自動的に使われなくなります。ブートストラップで判断したシグナルは trade_signals.policy_version が policy-vb（自己改善の提案が適用中なら policy-vb+<版>）になり、" +
		"ログ「policy: paper bootstrap entry decided on the legacy rule」でも分かります。保存した値は再起動不要で、次の評価から反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: paperbootstrapsettings.Key, label: "ブートストラップ（Paper 限定）", placeholder: "0（無効）",
			hint: "1 で有効、0 で無効です。既定は 0（無効）です。Paper 以外では 1 を保存できません。"},
	},
}

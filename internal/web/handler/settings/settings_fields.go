package settings

import "github.com/ousiassllc/pitha-trador/internal/config"

// settingsField is one Settings row: the secrets-table key and its label.
type settingsField struct {
	key   string
	label string
}

// connection is one external service the Settings screen manages (issue
// #302): the screen lists connections, and each opens a modal holding all
// of its keys. fields are in display order with the credential first - the
// first field is the "main" key whose state decides the connection's
// 設定済み/未設定 status (molecules.ConnectionProps.State) - followed by the
// optional URL/model overrides. Saving and deleting stay per key (`POST`/
// `DELETE /settings/:key`), so grouping never lets one field touch another.
type connection struct {
	id          string
	name        string
	description string
	// defaultLabel names what the connection uses while none of its fields
	// is stored (issue #273); "" for connections that need their own key.
	defaultLabel string
	fields       []settingsField
}

// settingsConnections is the Settings screen's connection list in display
// order. Labels are Japanese only (issue #902); the key itself is shown in
// the field's help mark (molecules.SecretFieldRow). Every key here must be
// in config.AllowedSecretKeys (the per-key routes' allow-list), and the
// groups together must cover it exactly once, which pageflow/groups_test.go
// asserts. Luna/Sol/Opus default to Jev and the news feed to the やのしん TDnet
// WebAPI (issue #273), so their fields are optional overrides only; the Jev
// key alone enables them.
var settingsConnections = []connection{
	{
		id:          "jev",
		name:        "Jev",
		description: "売買判断を行う AI（Jev）への接続です。API キーは必須です。接続先 URL とモデル名は未設定なら既定値を使います。",
		fields: []settingsField{
			{config.KeyJevAPIKey, "API キー"},
			{config.KeyJevBaseURL, "接続先 URL（任意）"},
			{config.KeyJevModel, "モデル名（任意）"},
		},
	},
	{
		id:          "kabu",
		name:        "kabuステーション",
		description: "kabuステーション API（時価・発注）の API パスワードです。必須です。",
		fields: []settingsField{
			{config.KeyKabuAPIPassword, "API パスワード"},
		},
	},
	{
		id:          "tachibana",
		name:        "立花証券 e支店",
		description: "立花証券 e支店API の認証情報です。認証ID は利用設定画面で発行されるもので、デモ環境と本番環境で別です。接続環境・秘密鍵ファイル・接続先 URL は、設定画面の「立花証券 e支店（接続設定）」で指定します。本番の第二暗証番号は保存できません。",
		fields: []settingsField{
			{config.KeyTachibanaDemoAuthID, "認証ID（デモ）"},
			{config.KeyTachibanaProdAuthID, "認証ID（本番）"},
			{config.KeyTachibanaDemoSecondPassword, "第二暗証番号（デモ・任意）"},
		},
	},
	{
		id:          "slack",
		name:        "Slack",
		description: "通知の送信先となる Slack の Incoming Webhook URL です（任意）。",
		fields: []settingsField{
			{config.KeySlackWebhookURL, "Webhook URL（任意）"},
		},
	},
	{
		id:           "luna",
		name:         "Luna",
		defaultLabel: "Jev",
		description:  "ニュースの分類を行う AI（Luna）への接続です。既定では Jev を使うため、追加のキー入力は不要です。別の AI に差し替える場合のみ入力してください（未入力なら Jev）。",
		fields: []settingsField{
			{config.KeyLunaAPIKey, "API キー（任意）"},
			{config.KeyLunaBaseURL, "接続先 URL（任意）"},
		},
	},
	{
		id:           "news-feed",
		name:         "ニュースフィード",
		defaultLabel: "やのしん",
		description:  "Luna に渡すニュースの取得元です。既定はやのしん TDnet WebAPI（適時開示、API キー不要）で、監視中の銘柄のみを取得します。別のフィードを使う場合は取得元 URL を入力し、止める場合は「取り込みの有効/無効」に off を保存してください。",
		fields: []settingsField{
			{config.KeyNewsFeedURL, "取得元 URL（任意）"},
			{config.KeyNewsFeedAPIKey, "API キー（任意）"},
			{config.KeyNewsFeedEnabled, "取り込みの有効/無効（任意）"},
		},
	},
	{
		id:           "sol",
		name:         "Sol",
		defaultLabel: "Jev",
		description:  "日次の自己改善で、しきい値の変更案を作る AI（Sol）への接続です。既定では Jev を使うため、追加のキー入力は不要です。別の AI に差し替える場合のみ入力してください（未入力なら Jev）。",
		fields: []settingsField{
			{config.KeySolAPIKey, "API キー（任意）"},
			{config.KeySolBaseURL, "接続先 URL（任意）"},
		},
	},
	{
		id:           "opus",
		name:         "Opus",
		defaultLabel: "Jev",
		description:  "Sol の改善提案を検証して承認・却下する AI（Opus）への接続です。既定では Jev を使うため、追加のキー入力は不要です。別の AI に差し替える場合のみ入力してください（未入力なら Jev）。",
		fields: []settingsField{
			{config.KeyOpusAPIKey, "API キー（任意）"},
			{config.KeyOpusBaseURL, "接続先 URL（任意）"},
		},
	},
}

// defaultedKeys are the override-only keys whose unset state is the normal
// one (a default applies), so the 要対応 list never nags about
// them.
var defaultedKeys = []string{
	config.KeyJevBaseURL, config.KeyJevModel,
	config.KeyLunaAPIKey, config.KeyLunaBaseURL, config.KeySolAPIKey, config.KeySolBaseURL, config.KeyOpusAPIKey, config.KeyOpusBaseURL,
	config.KeyNewsFeedURL, config.KeyNewsFeedAPIKey, config.KeyNewsFeedEnabled,
	config.KeyTachibanaDemoAuthID, config.KeyTachibanaProdAuthID, config.KeyTachibanaDemoSecondPassword,
}

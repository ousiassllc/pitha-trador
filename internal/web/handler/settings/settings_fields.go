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
	fields      []settingsField
}

// settingsConnections is the Settings screen's connection list in display
// order. Labels match the literal names operators previously set in `.env`
// so the migration away from it stays recognizable. Every key here must be
// in config.AllowedSecretKeys (the per-key routes' allow-list), and the
// groups together must cover it exactly once, which settings_display_test.go
// asserts. Luna/Sol/Opus/News Feed are shown as optional until issue #273
// (human decision) settles their default/hidden policy.
var settingsConnections = []connection{
	{
		id:          "jev",
		name:        "Jev",
		description: "売買判断を行う AI（Jev）への接続です。API キーは必須です。接続先 URL とモデル名は未設定なら既定値を使います。",
		fields: []settingsField{
			{config.KeyJevAPIKey, "JEV_API_KEY"},
			{config.KeyJevBaseURL, "JEV_BASE_URL（任意）"},
			{config.KeyJevModel, "JEV_MODEL（任意）"},
		},
	},
	{
		id:          "kabu",
		name:        "kabuステーション",
		description: "kabuステーション API（時価・発注）の API パスワードです。必須です。",
		fields: []settingsField{
			{config.KeyKabuAPIPassword, "KABU_API_PASSWORD"},
		},
	},
	{
		id:          "slack",
		name:        "Slack",
		description: "通知の送信先となる Slack の Incoming Webhook URL です（任意）。",
		fields: []settingsField{
			{config.KeySlackWebhookURL, "SLACK_WEBHOOK_URL（任意）"},
		},
	},
	{
		id:          "luna",
		name:        "Luna",
		description: "ニュースの分類・要約を行う AI（Luna）への接続です（任意）。",
		fields: []settingsField{
			{config.KeyLunaAPIKey, "LUNA_API_KEY（任意）"},
			{config.KeyLunaBaseURL, "LUNA_BASE_URL（任意）"},
		},
	},
	{
		id:          "news-feed",
		name:        "ニュースフィード",
		description: "Luna に渡すニュースを取得する外部ニュースフィードへの接続です（任意）。",
		fields: []settingsField{
			{config.KeyNewsFeedAPIKey, "NEWS_FEED_API_KEY（任意）"},
			{config.KeyNewsFeedURL, "NEWS_FEED_URL（任意）"},
		},
	},
	{
		id:          "sol",
		name:        "Sol",
		description: "負けトレードを分析して改善提案を作る AI（Sol）への接続です（任意）。",
		fields: []settingsField{
			{config.KeySolAPIKey, "SOL_API_KEY（任意）"},
			{config.KeySolBaseURL, "SOL_BASE_URL（任意）"},
		},
	},
	{
		id:          "opus",
		name:        "Opus",
		description: "Sol の改善提案を検証して承認・却下する AI（Opus）への接続です（任意）。",
		fields: []settingsField{
			{config.KeyOpusAPIKey, "OPUS_API_KEY（任意）"},
			{config.KeyOpusBaseURL, "OPUS_BASE_URL（任意）"},
		},
	},
}

// setupConnectionIDs are the connections the first-run Setup screen (issue
// #80) offers: the ones holding the two required keys plus Slack. The
// other connections are configured later on Settings.
var setupConnectionIDs = []string{"jev", "kabu", "slack"}

// defaultedKeys are the override-only keys whose unset state is the normal
// one (a default applies), so the global secrets banner never nags about
// them.
var defaultedKeys = []string{config.KeyJevBaseURL, config.KeyJevModel}

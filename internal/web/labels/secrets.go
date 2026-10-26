package labels

// ConnectionItemNames names the secrets-table keys (config.Key* of internal/config/
// secrets.go) for the screens that list a setting that is not stored yet. The
// screen never shows the key itself (issue #904).
var ConnectionItemNames = map[string]string{ //nolint:gosec // G101: names of the secrets-table keys, not credentials
	"JEV_API_KEY":                    "Jev の API キー",
	"JEV_BASE_URL":                   "Jev の接続先 URL",
	"JEV_MODEL":                      "Jev のモデル名",
	"KABU_API_PASSWORD":              "kabuステーションの API パスワード",
	"SLACK_WEBHOOK_URL":              "Slack の通知先（Webhook URL）",
	"LUNA_API_KEY":                   "Luna の API キー",
	"LUNA_BASE_URL":                  "Luna の接続先 URL",
	"NEWS_FEED_URL":                  "ニュースフィードの取得元 URL",
	"NEWS_FEED_API_KEY":              "ニュースフィードの API キー",
	"NEWS_FEED_ENABLED":              "ニュースの取り込みの有効/無効",
	"SOL_API_KEY":                    "Sol の API キー",
	"SOL_BASE_URL":                   "Sol の接続先 URL",
	"OPUS_API_KEY":                   "Opus の API キー",
	"OPUS_BASE_URL":                  "Opus の接続先 URL",
	"TACHIBANA_DEMO_AUTH_ID":         "立花証券の認証ID（デモ）",
	"TACHIBANA_PROD_AUTH_ID":         "立花証券の認証ID（本番）",
	"TACHIBANA_DEMO_SECOND_PASSWORD": "立花証券の第二暗証番号（デモ）",
}

// SecretName is the name of a secrets-table key.
func SecretName(key string) string {
	return lookup("secret_key", ConnectionItemNames, key, "その他の設定")
}

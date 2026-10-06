package settings

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// settingsHints is the per-key input guidance shown under a Settings
// field. Only keys whose expected format is easy to get wrong have one.
var settingsHints = map[string]string{
	config.KeyJevBaseURL: "未設定の場合は既定値 " + jev.DefaultBaseURL + " を使用します。上書きする場合はホスト名のみ入力してください（/v1/systemone などのパスは付けないでください）。",
	config.KeyJevModel:   "未設定の場合は既定値 " + jev.DefaultModel + " を使用します。",

	config.KeyLunaBaseURL:     assistOverrideHint("Luna", "/v1/classify"),
	config.KeySolBaseURL:      assistOverrideHint("Sol", "/v1/analyze"),
	config.KeyOpusBaseURL:     assistOverrideHint("Opus", "/v1/review"),
	config.KeyNewsFeedURL:     "未設定の場合はやのしん TDnet WebAPI（https://webapi.yanoshin.jp/）を使用します。差し替える場合は {\"items\":[{id,headline,body,published_at}]} を返す GET エンドポイントの URL を入力してください（?symbol= が付きます）。",
	config.KeyNewsFeedEnabled: "未設定または on でニュース取り込みを行います。off を保存すると停止します（再起動後に反映）。",
}

// assistOverrideHint is the hint of a role's BASE_URL override: an unset role
// uses Jev, and the override's API contract takes no model name, so there is
// no model field (issue #273).
func assistOverrideHint(role, path string) string {
	return "未設定の場合は " + role + " も Jev を使用します。別の AI に差し替える場合のみ、" + path + " を提供する API のホスト名を入力してください（この API はモデル名を受け取らないため、モデル名の上書きはありません）。"
}

package settings

import "github.com/ousiassllc/pitha-trador/internal/config"

// settingsField is one Settings row: the secrets-table key and its label.
type settingsField struct {
	key   string
	label string
}

// settingsFields is the Settings screen's always-visible keys in display
// order (issue #57 スコープ item 6, regrouped by issue #272): the
// credentials an operator actually has to enter. Label matches the literal
// names operators previously set in `.env` so the migration away from it
// stays recognizable. Every key here and in advancedSettingsFields must be
// in config.AllowedSecretKeys (the per-key routes' allow-list), and
// together they must cover it exactly, which settings_display_test.go
// asserts.
var settingsFields = []settingsField{
	{config.KeyJevAPIKey, "JEV_API_KEY"},
	{config.KeyKabuAPIPassword, "KABU_API_PASSWORD"},
	{config.KeySlackWebhookURL, "SLACK_WEBHOOK_URL（任意）"},
	{config.KeyLunaAPIKey, "LUNA_API_KEY（任意）"},
	{config.KeyNewsFeedAPIKey, "NEWS_FEED_API_KEY（任意）"},
	{config.KeySolAPIKey, "SOL_API_KEY（任意）"},
	{config.KeyOpusAPIKey, "OPUS_API_KEY（任意）"},
}

// advancedSettingsFields is the collapsed 「詳細設定（任意）」 section
// (issue #272): endpoint/model overrides and other rarely needed values.
// Leaving one unset means "use the default" and a stored override is
// reverted by its own delete button. The Luna/Sol/Opus/News Feed keys stay
// here as they are until issue #273 decides their final treatment.
var advancedSettingsFields = []settingsField{
	{config.KeyJevBaseURL, "JEV_BASE_URL（任意）"},
	{config.KeyJevModel, "JEV_MODEL（任意）"},
	{config.KeyLunaBaseURL, "LUNA_BASE_URL（任意）"},
	{config.KeyNewsFeedURL, "NEWS_FEED_URL（任意）"},
	{config.KeySolBaseURL, "SOL_BASE_URL（任意）"},
	{config.KeyOpusBaseURL, "OPUS_BASE_URL（任意）"},
}

// defaultedKeys are the override-only keys whose unset state is the normal
// one (a default applies), so the global secrets banner never nags about
// them.
var defaultedKeys = []string{config.KeyJevBaseURL, config.KeyJevModel}

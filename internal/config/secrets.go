package config

import (
	"context"
	"fmt"
	"slices"
)

// Key* are the secrets table row keys internal/repository/system.SecretsRepository
// stores and internal/web/handler/settings's Settings screen (`GET /settings`, `POST`/`DELETE
// /settings/:key`) reads and writes. Before issue #57 these were environment
// variable names (JEV_API_KEY etc., read by the now-removed LoadSecrets
// via os.Getenv); the string values are kept identical across that
// migration so the existing .env.example history and the Settings
// screen's field labels stay recognizable, but nothing in this codebase
// calls os.Getenv for any of them anymore - they are DB row keys now,
// not env var names.
const (
	// KeyJevAPIKey supplies internal/service/jev.Config's APIKey
	// (docs/architecture/overview.md §6). It is required.
	KeyJevAPIKey = "JEV_API_KEY"
	// KeyJevBaseURL and KeyJevModel supply internal/service/jev.Config's
	// BaseURL/Model overrides (issues #271, #274). Both are optional: an
	// unset (or empty) value makes the jev client use its own default
	// (jev.DefaultBaseURL / jev.DefaultModel), and a stored value takes
	// precedence over that default until it is deleted.
	KeyJevBaseURL = "JEV_BASE_URL"
	KeyJevModel   = "JEV_MODEL"
	// KeyKabuAPIPassword supplies internal/service/marketdata.Config's
	// APIPassword field. It must match the APIPassword configured inside
	// the kabuステーションアプリ itself (docs/architecture/overview.md
	// §5). Until Phase 7 it is the single, market-data-only kabu credential
	// (no order endpoint is ever called); the Production/Paper split
	// required by docs/requirements/non-functional.md §4 is added together
	// with the order endpoints, not before.
	KeyKabuAPIPassword = "KABU_API_PASSWORD"
	// KeySlackWebhookURL supplies internal/service/notify.Config's
	// WebhookURL (non-functional.md §5.2's immediate alerts). Unlike
	// KeyJevAPIKey and KeyKabuAPIPassword it is optional: LoadSecretsFromDB never reports it
	// missing, and internal/bootstrap simply skips the Slack channel when
	// it is empty (Paper Trading has no real-money exposure to alert on,
	// and a dev machine without a webhook must still be able to start).
	KeySlackWebhookURL = "SLACK_WEBHOOK_URL"
	// KeyLunaAPIKey/KeyLunaBaseURL supply internal/service/assist's Luna
	// adapter (ニュース分類, FR-LUNA-2). They are optional overrides: Luna
	// defaults to Jev (issue #273) and only calls its own API once
	// LUNA_BASE_URL is set. KeyNewsFeedURL/KeyNewsFeedAPIKey override
	// internal/service/newsfeed's default feed (やのしん TDnet WebAPI, no
	// key needed) with a user-specified generic feed (FR-LUNA-1), and
	// KeyNewsFeedEnabled switches News Ingest off ("off"; unset = on).
	// With Luna and the feed both available News Ingest runs; otherwise it
	// does not and no news flag is ever raised (FR-LUNA-4).
	KeyLunaAPIKey     = "LUNA_API_KEY"
	KeyLunaBaseURL    = "LUNA_BASE_URL"
	KeyNewsFeedURL    = "NEWS_FEED_URL"
	KeyNewsFeedAPIKey = "NEWS_FEED_API_KEY"
	// KeyNewsFeedEnabled is "on" or "off" (see NormalizeSecretValue); only
	// "off" has an effect (Secrets.NewsFeedOff).
	KeyNewsFeedEnabled = "NEWS_FEED_ENABLED"
	// KeySolAPIKey/KeySolBaseURL and KeyOpusAPIKey/KeyOpusBaseURL are the
	// optional overrides of internal/service/assist's Sol (daily analysis)
	// and Opus (proposal review) adapters (FR-SELFIMPROVE-8/9). Both
	// default to Jev (issue #273); a role with its BASE_URL set calls that
	// API instead, an unset role falls back to Jev. With Jev unconfigured
	// as well the matching stage is skipped each day.
	KeySolAPIKey   = "SOL_API_KEY"
	KeySolBaseURL  = "SOL_BASE_URL"
	KeyOpusAPIKey  = "OPUS_API_KEY"
	KeyOpusBaseURL = "OPUS_BASE_URL"
	// KeyTachibanaDemoAuthID / KeyTachibanaProdAuthID are the 認証ID of the
	// 立花証券 e支店API (issued per environment on its 利用設定 screen), and
	// KeyTachibanaDemoSecondPassword the デモ環境's 第二暗証番号 (used only by
	// the optional demo order smoke, issue #725). They are optional keys in
	// the static sense - the Setup Guard requires the selected broker's and
	// environment's ID (RequiredSetup, issue #734). There is deliberately no
	// 本番 第二暗証番号 key: it arrives with the Production/Paper split of
	// issue #55, and Secrets.TachibanaCredentials never returns one for
	// TachibanaEnvProduction.
	KeyTachibanaDemoAuthID         = "TACHIBANA_DEMO_AUTH_ID"
	KeyTachibanaProdAuthID         = "TACHIBANA_PROD_AUTH_ID"
	KeyTachibanaDemoSecondPassword = "TACHIBANA_DEMO_SECOND_PASSWORD"
)

// Switch values of KeyNewsFeedEnabled.
const (
	SwitchOn  = "on"
	SwitchOff = "off"
)

// requiredSecretKeys are the Settings fields whose absence
// LoadSecretsFromDB reports in its missing return value. KeySlackWebhookURL
// and the Luna/News Feed keys are intentionally excluded - see
// optionalSecretKeys.
var requiredSecretKeys = []string{KeyJevAPIKey, KeyKabuAPIPassword}

// optionalSecretKeys are loaded like requiredSecretKeys but never reported
// as missing.
var optionalSecretKeys = []string{KeyJevBaseURL, KeyJevModel, KeySlackWebhookURL, KeyLunaAPIKey, KeyLunaBaseURL, KeyNewsFeedURL, KeyNewsFeedAPIKey, KeyNewsFeedEnabled,
	KeySolAPIKey, KeySolBaseURL, KeyOpusAPIKey, KeyOpusBaseURL,
	KeyTachibanaDemoAuthID, KeyTachibanaProdAuthID, KeyTachibanaDemoSecondPassword}

// RequiredSecretKeys returns the keys whose absence keeps the app
// unusable with the default kabu broker (JEV_API_KEY/KABU_API_PASSWORD): the
// Setup Guard redirects to `/setup` until every one is stored (issues #80,
// #271). RequiredSetup derives the broker-dependent set (issue #734).
func RequiredSecretKeys() []string {
	return append([]string{}, requiredSecretKeys...)
}

// OptionalSecretKeys returns every allowed key that is not required.
func OptionalSecretKeys() []string {
	return append([]string{}, optionalSecretKeys...)
}

// AllowedSecretKeys returns every secrets-table key the Settings screen
// may save or delete (`POST`/`DELETE /settings/:key`): the required keys
// followed by the optional ones. It is the single allow-list for the
// per-key Settings routes; any other key name is rejected with 400.
func AllowedSecretKeys() []string {
	return append(append([]string{}, requiredSecretKeys...), optionalSecretKeys...)
}

// IsAllowedSecretKey reports whether key is in AllowedSecretKeys.
func IsAllowedSecretKey(key string) bool {
	return slices.Contains(requiredSecretKeys, key) || slices.Contains(optionalSecretKeys, key)
}

// Secrets holds every credential internal/bootstrap.BuildServices passes
// into internal/service/marketdata.Config and internal/service/jev.Config
// (plus internal/service/notify's Slack channel). LoadSecretsFromDB
// populates it from the DB-persisted, encrypted `secrets` table (issue
// #57); this struct itself never touches disk or the process environment
// directly.
type Secrets struct {
	JevAPIKey string
	// JevBaseURL and JevModel are optional overrides (see KeyJevBaseURL);
	// empty means "use the jev client's default".
	JevBaseURL      string
	JevModel        string
	KabuAPIPassword string
	// SlackWebhookURL is optional (see KeySlackWebhookURL); empty means
	// "no Slack channel".
	SlackWebhookURL string
	// LunaAPIKey/LunaBaseURL and NewsFeedURL/NewsFeedAPIKey are optional
	// (see KeyLunaAPIKey); empty means News Ingest/Luna are disabled.
	LunaAPIKey     string
	LunaBaseURL    string
	NewsFeedURL    string
	NewsFeedAPIKey string
	// NewsFeedEnabled is the stored KeyNewsFeedEnabled value ("" = unset).
	NewsFeedEnabled string
	// SolAPIKey/SolBaseURL and OpusAPIKey/OpusBaseURL are optional (see
	// KeySolAPIKey).
	SolAPIKey   string
	SolBaseURL  string
	OpusAPIKey  string
	OpusBaseURL string
	// TachibanaDemoAuthID / TachibanaProdAuthID are the 立花 認証ID per
	// environment. The デモ 第二暗証番号 is unexported so it can only leave
	// Secrets through TachibanaCredentials, which withholds it in 本番.
	TachibanaDemoAuthID         string
	TachibanaProdAuthID         string
	tachibanaDemoSecondPassword string
}

// TachibanaCredentials are the secrets the 立花 adapter uses in one
// environment. SecondPassword is empty in 本番: the app never holds a
// production 第二暗証番号 before issue #55, so no request it builds can carry
// one.
type TachibanaCredentials struct {
	AuthID         string
	SecondPassword string
}

// TachibanaCredentials returns the credentials of environment env
// (TachibanaEnvDemo or TachibanaEnvProduction; anything else is treated as
// production, the stricter one).
func (s Secrets) TachibanaCredentials(env string) TachibanaCredentials {
	if env == TachibanaEnvDemo {
		return TachibanaCredentials{AuthID: s.TachibanaDemoAuthID, SecondPassword: s.tachibanaDemoSecondPassword}
	}
	return TachibanaCredentials{AuthID: s.TachibanaProdAuthID}
}

// NewsFeedOff reports whether the operator switched the news feed off
// (NEWS_FEED_ENABLED=off): News Ingest is then not started. Unset or "on"
// keeps the default (やのしん, or NEWS_FEED_URL when set).
func (s Secrets) NewsFeedOff() bool {
	return s.NewsFeedEnabled == SwitchOff
}

// SecretsRepository is the subset of internal/repository/system.SecretsRepository's
// methods LoadSecretsFromDB needs. An interface here - rather than
// importing internal/repository's concrete type - keeps this package
// dependency-free (see doc.go: config MUST NOT depend on any other
// internal package); *system.SecretsRepository implements it without
// either package needing to import the other, the same
// interface-at-the-consumer pattern internal/web/handler already uses
// for its own SystemEngine/CalibrationSource types wrapping concrete
// internal/service implementations.
type SecretsRepository interface {
	// Get returns key's decrypted plaintext value, or ("", false, nil)
	// when no value has been stored for it yet.
	Get(ctx context.Context, key string) (plaintext string, ok bool, err error)
}

// LoadSecretsFromDB reads Secrets from repo's secrets table instead of
// the process environment issue #43's original LoadSecrets used
// (env-var support was removed entirely by issue #57: `.env` no longer
// carries the secrets-table keys such as JEV_API_KEY/KABU_API_PASSWORD).
// Unlike LoadSecrets, an unset value is never an error: missing names
// every key among KeyJevAPIKey/KeyKabuAPIPassword that has
// no stored value yet, letting the caller (cmd/desktop, cmd/server) log
// a warning and start anyway - Jev/kabuステーションAPI-dependent
// features simply error at call time until an operator fills them in
// via the Settings screen (`GET /settings`) and restarts the app (no
// hot-reload, issue #57's decision - internal/bootstrap.BuildServices
// still wires the value it was given exactly once). err is non-nil only
// for an actual repository/DB failure.
func LoadSecretsFromDB(ctx context.Context, repo SecretsRepository) (Secrets, []string, error) {
	values := make(map[string]string, len(requiredSecretKeys)+len(optionalSecretKeys))
	for _, key := range AllowedSecretKeys() {
		value, _, err := repo.Get(ctx, key)
		if err != nil {
			return Secrets{}, nil, fmt.Errorf("config: load secret %q: %w", key, err)
		}
		values[key] = value
	}

	var missing []string
	for _, key := range requiredSecretKeys {
		if values[key] == "" {
			missing = append(missing, key)
		}
	}

	return Secrets{
		JevAPIKey:       values[KeyJevAPIKey],
		JevBaseURL:      values[KeyJevBaseURL],
		JevModel:        values[KeyJevModel],
		KabuAPIPassword: values[KeyKabuAPIPassword],
		SlackWebhookURL: values[KeySlackWebhookURL],
		LunaAPIKey:      values[KeyLunaAPIKey],
		LunaBaseURL:     values[KeyLunaBaseURL],
		NewsFeedURL:     values[KeyNewsFeedURL],
		NewsFeedAPIKey:  values[KeyNewsFeedAPIKey],
		NewsFeedEnabled: values[KeyNewsFeedEnabled],
		SolAPIKey:       values[KeySolAPIKey],
		SolBaseURL:      values[KeySolBaseURL],
		OpusAPIKey:      values[KeyOpusAPIKey],
		OpusBaseURL:     values[KeyOpusBaseURL],

		TachibanaDemoAuthID:         values[KeyTachibanaDemoAuthID],
		TachibanaProdAuthID:         values[KeyTachibanaProdAuthID],
		tachibanaDemoSecondPassword: values[KeyTachibanaDemoSecondPassword],
	}, missing, nil
}

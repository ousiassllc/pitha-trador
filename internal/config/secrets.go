package config

import (
	"context"
	"fmt"
)

// Key* are the secrets table row keys internal/repository.SecretsRepository
// stores and internal/web/handler's Settings screen (`GET`/`POST
// /settings`) reads and writes. Before issue #57 these were environment
// variable names (JEV_API_KEY etc., read by the now-removed LoadSecrets
// via os.Getenv); the string values are kept identical across that
// migration so the existing .env.example history and the Settings
// screen's field labels stay recognizable, but nothing in this codebase
// calls os.Getenv for any of them anymore - they are DB row keys now,
// not env var names.
const (
	// KeyJevAPIKey and KeyJevBaseURL supply internal/service/jev.Config's
	// APIKey/BaseURL fields (docs/architecture/overview.md §6).
	KeyJevAPIKey  = "JEV_API_KEY"
	KeyJevBaseURL = "JEV_BASE_URL"
	// KeyKabuAPIPassword supplies internal/service/marketdata.Config's
	// APIPassword field. It must match the APIPassword configured inside
	// the kabuステーションアプリ itself (docs/architecture/overview.md
	// §5).
	KeyKabuAPIPassword = "KABU_API_PASSWORD"
	// KeySlackWebhookURL supplies internal/service/notify.Config's
	// WebhookURL (non-functional.md §5.2's immediate alerts). Unlike the
	// three keys above it is optional: LoadSecretsFromDB never reports it
	// missing, and internal/bootstrap simply skips the Slack channel when
	// it is empty (Paper Trading has no real-money exposure to alert on,
	// and a dev machine without a webhook must still be able to start).
	KeySlackWebhookURL = "SLACK_WEBHOOK_URL"
)

// requiredSecretKeys are the Settings fields whose absence
// LoadSecretsFromDB reports in its missing return value. KeySlackWebhookURL
// is intentionally excluded - see Secrets.SlackWebhookURL.
var requiredSecretKeys = []string{KeyJevAPIKey, KeyJevBaseURL, KeyKabuAPIPassword}

// Secrets holds every credential internal/bootstrap.BuildServices passes
// into internal/service/marketdata.Config and internal/service/jev.Config
// (plus internal/service/notify's Slack channel). LoadSecretsFromDB
// populates it from the DB-persisted, encrypted `secrets` table (issue
// #57); this struct itself never touches disk or the process environment
// directly.
type Secrets struct {
	JevAPIKey       string
	JevBaseURL      string
	KabuAPIPassword string
	// SlackWebhookURL is optional (see KeySlackWebhookURL); empty means
	// "no Slack channel".
	SlackWebhookURL string
}

// SecretsRepository is the subset of internal/repository.SecretsRepository's
// methods LoadSecretsFromDB needs. An interface here - rather than
// importing internal/repository's concrete type - keeps this package
// dependency-free (see doc.go: config MUST NOT depend on any other
// internal package); *repository.SecretsRepository implements it without
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
// carries JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL).
// Unlike LoadSecrets, an unset value is never an error: missing names
// every key among KeyJevAPIKey/KeyJevBaseURL/KeyKabuAPIPassword that has
// no stored value yet, letting the caller (cmd/desktop, cmd/server) log
// a warning and start anyway - Jev/kabuステーションAPI-dependent
// features simply error at call time until an operator fills them in
// via the Settings screen (`GET /settings`) and restarts the app (no
// hot-reload, issue #57's decision - internal/bootstrap.BuildServices
// still wires the value it was given exactly once). err is non-nil only
// for an actual repository/DB failure.
func LoadSecretsFromDB(ctx context.Context, repo SecretsRepository) (Secrets, []string, error) {
	values := make(map[string]string, len(requiredSecretKeys)+1)
	for _, key := range append(append([]string{}, requiredSecretKeys...), KeySlackWebhookURL) {
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
		KabuAPIPassword: values[KeyKabuAPIPassword],
		SlackWebhookURL: values[KeySlackWebhookURL],
	}, missing, nil
}

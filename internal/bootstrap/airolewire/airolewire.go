// Package airolewire turns the AI provider layer (internal/config/airole, issue
// #1080) into the clients the composition root builds: it resolves the live Jev
// role's connection once at start-up from the stored role settings and the
// stored secrets (read only: nothing here writes a setting or a secret), gives
// Sol, Opus and Luna a backend that re-reads their setting on every call (issue
// #1112), fills in the connections of a trial arm's provider, and records which
// provider and model each role runs on.
//
// With nothing stored every role is on the custom provider, which is exactly the
// connection it had before the layer existed. A provider or model saved for Sol,
// Opus or Luna applies to their next call; the live Jev (what is traded) and the
// secrets themselves (keys, URLs) change only with a restart.
package airolewire

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/airoles"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// Roles is every role's connection as resolved at start-up (the live Jev's is the
// one in use until a restart), the secrets and settings Sol, Opus and Luna
// resolve against on every call, and the store of the roles' settings and history.
type Roles struct {
	conns    map[string]airole.Connection
	settings airole.Getter
	secrets  config.Secrets
	keys     config.SecretsRepository // nil: Sol, Opus and Luna keep the start-up OpenRouter key and URL
	store    *system.AIRoleRepository
}

// Resolve reads each role's setting from settings (runtime_settings, read only) and
// resolves it against secrets. A setting that cannot be read or parsed falls back
// to the default (live Jev) or is skipped at call time (Sol, Opus, Luna), never
// moved to a different provider. db holds the roles' history.
// The result keeps settings and secrets: Sol, Opus and Luna read their setting
// again on every call (Backend).
func Resolve(ctx context.Context, db *sql.DB, settings airole.Getter, secrets config.Secrets) Roles {
	loaded := airole.Load(ctx, settings)
	for role, why := range loaded.Damaged {
		slog.Warn("bootstrap: ai role setting ignored; the live Jev runs on the default, Sol, Opus and Luna are skipped", "role", role, "reason", why)
	}
	conns := make(map[string]airole.Connection, len(loaded.Settings))
	for role, set := range loaded.Settings {
		conns[role] = airole.Resolve(role, set, secrets)
	}
	return Roles{conns: conns, settings: settings, secrets: secrets, store: system.NewAIRoleRepository(db)}
}

// WithSecretStore makes Sol, Opus and Luna read the OpenRouter key and base URL
// from keys on every call, so a key saved on the Settings screen applies to their
// next call too. The start-up value is not used as a fallback when the read fails:
// the role is skipped for that call. The live Jev keeps its start-up connection.
func (r Roles) WithSecretStore(keys config.SecretsRepository) Roles {
	r.keys = keys
	return r
}

// Settings is the Settings screen's service over the roles' settings and history;
// keys says whether each provider's key is set (never its value).
func (r Roles) Settings(settings airole.Getter, keys airoles.KeyStore) *airoles.Service {
	return airoles.New(settings, r.store, keys)
}

// Connection is role's connection as resolved at start-up.
func (r Roles) Connection(role string) airole.Connection { return r.conns[role] }

// Backend is the backend of a Sol, Opus or Luna role, resolved on every call
// from the role's setting as stored at that moment (issue #1112): the override
// client and the Jev-protocol fallback. own is the role's own SOL_*/OPUS_*/LUNA_*
// override and live the live Jev client. On the custom provider that is the
// connection the role always had (its own override, else the live Jev). On
// OpenRouter the override is dropped and the fallback is a client for OpenRouter
// with the role's key and model, so the role is never served by the live Jev
// connection: without an OpenRouter key it is skipped, not silently moved to
// another provider. A stored value that cannot be read or parsed skips the role
// as well (an error wrapping assist.ErrNotConfigured); it is never read as
// "nothing saved". Keys and URLs are never logged.
func (r Roles) Backend(role string, own assist.Config, live assist.Asker) assist.Backend {
	custom := assist.NewClient(own)
	own.BaseURL, own.APIKey = "", ""
	none := assist.NewClient(own) // not configured: the role is served by its fallback only
	var (
		mu     sync.Mutex
		cached airole.Connection
		asker  assist.Asker
	)
	return func(ctx context.Context) (*assist.Client, assist.Asker, error) {
		c, err := r.current(ctx, role)
		if err != nil {
			slog.Warn("airolewire: ai role skipped: its stored setting is unusable", "role", role, "reason", err.Error())
			return nil, nil, fmt.Errorf("%w: %s setting is unusable", assist.ErrNotConfigured, role)
		}
		if c.Delegated {
			return custom, live, nil
		}
		mu.Lock()
		defer mu.Unlock()
		if asker == nil || c != cached {
			cached, asker = c, jev.NewClient(jev.Config{BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model})
		}
		return none, asker, nil
	}
}

// current is role's connection from the setting (and, with WithSecretStore, the
// OpenRouter key and URL) stored now. The reason of an error never contains a
// stored value.
func (r Roles) current(ctx context.Context, role string) (airole.Connection, error) {
	set := airole.DefaultSetting()
	raw, ok, err := r.settings.Get(ctx, airole.Key(role))
	if err != nil {
		return airole.Connection{}, errors.New("cannot read the stored setting")
	}
	if ok {
		if set, err = airole.Parse(role, raw); err != nil {
			return airole.Connection{}, errors.New("the stored setting is invalid")
		}
	}
	secrets := r.secrets
	if set.Provider == airole.ProviderOpenRouter && r.keys != nil {
		if secrets.OpenRouterAPIKey, _, err = r.keys.Get(ctx, config.KeyOpenRouterAPIKey); err != nil {
			return airole.Connection{}, errors.New("cannot read the stored OpenRouter key")
		}
		if secrets.OpenRouterBaseURL, _, err = r.keys.Get(ctx, config.KeyOpenRouterBaseURL); err != nil {
			return airole.Connection{}, errors.New("cannot read the stored OpenRouter URL")
		}
	}
	return airole.Resolve(role, set, secrets), nil
}

// Luna is Backend for the Luna role, in the form assist.NewLuna takes it
// (assist.NewLuna(nil, roles.Luna(...))).
func (r Roles) Luna(own assist.Config, live assist.Asker) assist.Option {
	return assist.WithBackend(r.Backend(airole.RoleLuna, own, live))
}

// Sol is Backend for the Sol role, in the form assist.NewSol takes it.
func (r Roles) Sol(own assist.Config, live assist.Asker) assist.Option {
	return assist.WithBackend(r.Backend(airole.RoleSol, own, live))
}

// Opus is Backend for the Opus role, in the form assist.NewOpus takes it.
func (r Roles) Opus(own assist.Config, live assist.Asker) assist.Option {
	return assist.WithBackend(r.Backend(airole.RoleOpus, own, live))
}

// JevConfig is base with the live Jev role's connection (URL, key, model) filled
// in, as resolved at start-up: the live Jev is not hot reloaded.
func (r Roles) JevConfig(base jev.Config) jev.Config {
	c := r.conns[airole.RoleJev]
	base.BaseURL, base.APIKey, base.Model = c.BaseURL, c.APIKey, c.Model
	return base
}

// Environment variables of the ChatGPT subscription provider (issue #1081). They
// name where the Codex CLI and its dedicated CODEX_HOME are; they hold no secret.
// Without EnvCodexHome no Codex arm is ever called (the person's own Codex home is
// never used).
const (
	EnvCodexHome = "PITHA_CODEX_HOME"
	EnvCodexBin  = "PITHA_CODEX_BIN"
)

// TrialKeys are the connections of the providers a trial arm can name:
// OpenRouter as before, the custom provider (the Jev connection's URL and
// key, whose default URL is filled in here because the trial client's own default
// is OpenRouter's) and the Codex CLI (EnvCodexHome, EnvCodexBin).
func TrialKeys(secrets config.Secrets) trial.Keys {
	custom := trial.Endpoint{BaseURL: secrets.JevBaseURL, APIKey: secrets.JevAPIKey}
	if custom.BaseURL == "" {
		custom.BaseURL = jev.DefaultBaseURL
	}
	return trial.Keys{APIKey: secrets.OpenRouterAPIKey, BaseURL: secrets.OpenRouterBaseURL, Custom: custom,
		Codex: trial.CodexConn{Bin: os.Getenv(EnvCodexBin), Home: os.Getenv(EnvCodexHome)}}
}

// Record records the provider and model each role runs on since this start
// (only a change from the last record writes a row). A failure is logged and
// does not stop the start.
func (r Roles) Record(ctx context.Context) {
	for _, role := range airole.Roles() {
		c := r.conns[role]
		running := airole.Setting{Provider: c.Provider, Model: c.Model}
		if role == airole.RoleJev && running.Model == "" {
			running.Model = jev.DefaultModel // the client's own default
		}
		if _, err := r.store.RecordStarted(ctx, role, running); err != nil {
			slog.Warn("bootstrap: record ai role", "role", role, "error", err)
		}
	}
}

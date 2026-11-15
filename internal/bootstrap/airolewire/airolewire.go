// Package airolewire turns the AI provider layer (internal/config/airole, issue
// #1080) into the clients the composition root builds: it resolves every role's
// connection once at start-up from the stored role settings and the stored
// secrets (read only: nothing here writes a setting or a secret), gives Sol, Opus
// and Luna their override client and Jev-protocol fallback, fills in the
// connections of a trial arm's provider, and records which provider and model
// each role runs on.
//
// With nothing stored every role is on the custom provider, which is exactly the
// connection it had before the layer existed. A change needs a restart, like the
// secrets themselves.
package airolewire

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// Roles is every role's resolved connection.
type Roles struct {
	conns map[string]airole.Connection
}

// Resolve reads each role's setting from g (runtime_settings, read only) and
// resolves it against secrets. A setting that cannot be read or parsed falls back
// to the default, never to a different provider.
func Resolve(ctx context.Context, g airole.Getter, secrets config.Secrets) Roles {
	loaded := airole.Load(ctx, g)
	for role, why := range loaded.Damaged {
		slog.Warn("bootstrap: ai role setting ignored; running on the default", "role", role, "reason", why)
	}
	conns := make(map[string]airole.Connection, len(loaded.Settings))
	for role, set := range loaded.Settings {
		conns[role] = airole.Resolve(role, set, secrets)
	}
	return Roles{conns: conns}
}

// Connection is role's connection.
func (r Roles) Connection(role string) airole.Connection { return r.conns[role] }

// AssistBackend returns the override client and the Jev-protocol fallback of a
// Sol, Opus or Luna role. own is the role's own SOL_*/OPUS_*/LUNA_* override and
// live the live Jev client. On the custom provider that is the connection the
// role always had (its own override, else the live Jev). On OpenRouter the
// override is dropped and the fallback is a client for OpenRouter with the
// role's key and model, so the role is never served by the live Jev connection:
// without an OpenRouter key it is skipped, not silently moved to another provider.
func (r Roles) AssistBackend(role string, own assist.Config, live assist.Asker) (*assist.Client, assist.Asker) {
	c := r.conns[role]
	if c.Delegated {
		return assist.NewClient(own), live
	}
	own.BaseURL, own.APIKey = "", ""
	return assist.NewClient(own), jev.NewClient(jev.Config{BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model})
}

// Assist is AssistBackend in the form NewLuna, NewSol and NewOpus take their arguments: the override
// client and the Jev-protocol fallback as the option that sets it.
func (r Roles) Assist(role string, own assist.Config, live assist.Asker) (*assist.Client, assist.Option) {
	client, asker := r.AssistBackend(role, own, live)
	return client, assist.WithJev(asker)
}

// JevConfig is base with the live Jev role's connection (URL, key, model) filled in.
func (r Roles) JevConfig(base jev.Config) jev.Config {
	c := r.conns[airole.RoleJev]
	base.BaseURL, base.APIKey, base.Model = c.BaseURL, c.APIKey, c.Model
	return base
}

// TrialKeys are the connections of the providers a trial arm can name:
// OpenRouter as before, and the custom provider (the Jev connection's URL and
// key, whose default URL is filled in here because the trial client's own default
// is OpenRouter's).
func TrialKeys(secrets config.Secrets) trial.Keys {
	custom := trial.Endpoint{BaseURL: secrets.JevBaseURL, APIKey: secrets.JevAPIKey}
	if custom.BaseURL == "" {
		custom.BaseURL = jev.DefaultBaseURL
	}
	return trial.Keys{APIKey: secrets.OpenRouterAPIKey, BaseURL: secrets.OpenRouterBaseURL, Custom: custom}
}

// Recorder is the history store; *system.AIRoleRepository implements it.
type Recorder interface {
	RecordStarted(ctx context.Context, role string, running airole.Setting) (bool, error)
}

// Record records the provider and model each role runs on since this start
// (only a change from the last record writes a row). A failure is logged and
// does not stop the start.
func (r Roles) Record(ctx context.Context, rec Recorder) {
	for _, role := range airole.Roles() {
		c := r.conns[role]
		running := airole.Setting{Provider: c.Provider, Model: c.Model}
		if role == airole.RoleJev && running.Model == "" {
			running.Model = jev.DefaultModel // the client's own default
		}
		if _, err := rec.RecordStarted(ctx, role, running); err != nil {
			slog.Warn("bootstrap: record ai role", "role", role, "error", err)
		}
	}
}

package airole

import "github.com/ousiassllc/pitha-trador/internal/config"

// Connection is the connection one role calls: where, with which key and
// which model. The APIKey is a secret: it is held in memory only and never
// printed, logged or stored.
type Connection struct {
	Role     string
	Provider string
	// BaseURL, APIKey and Model are the values to give the client. For the
	// custom provider of Sol/Opus/Luna they are empty: those roles keep their
	// own SOL_*/OPUS_*/LUNA_* override client and the Jev fallback (Delegated).
	BaseURL string
	APIKey  string
	Model   string
	// Delegated is true for the custom provider of Sol, Opus and Luna: the
	// caller keeps the connection those roles had before issue #1080.
	Delegated bool
}

// Resolve turns role's setting and the stored secrets into its connection. It
// only reads secrets. The custom provider of Jev is the live Jev connection as
// before (JEV_BASE_URL and JEV_API_KEY; the model is the setting's, else
// JEV_MODEL); OpenRouter is OPENROUTER_API_KEY and OPENROUTER_BASE_URL (the
// public endpoint when unset) with the setting's model.
func Resolve(role string, s Setting, secrets config.Secrets) Connection {
	if s.Provider == ProviderOpenRouter {
		base := secrets.OpenRouterBaseURL
		if base == "" {
			base = DefaultOpenRouterBaseURL
		}
		return Connection{Role: role, Provider: ProviderOpenRouter, BaseURL: base, APIKey: secrets.OpenRouterAPIKey, Model: s.Model}
	}
	if role != RoleJev {
		return Connection{Role: role, Provider: ProviderCustom, Delegated: true}
	}
	model := s.Model
	if model == "" {
		model = secrets.JevModel
	}
	return Connection{Role: role, Provider: ProviderCustom, BaseURL: secrets.JevBaseURL, APIKey: secrets.JevAPIKey, Model: model}
}

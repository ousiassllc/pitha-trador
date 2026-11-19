// Package airole defines the AI provider layer of issue #1080: which provider
// (独自 API = the Jev connection, or OpenRouter) each AI role calls and with
// which model, and how a role's setting plus the stored connection secrets
// resolve into one connection (Resolve).
//
// A role's setting lives in runtime_settings under ai.role.<role> (a JSON
// Setting) and is written by the Settings screen only. None of the keys is on
// the MCP's write whitelist (mcpwritesettings): the live Jev role decides what
// is traded, and the other roles feed the self-improvement loop and the news
// flags, so changing a provider or model is a person's decision on the screen.
// Nothing stored means the default: every role keeps the connection it had
// before this layer existed (ProviderCustom, i.e. JEV_* for Jev and the role's
// own SOL_*/OPUS_*/LUNA_* override, or Jev, for the others).
//
// The connection secrets themselves (JEV_*, OPENROUTER_*) stay where they are:
// this package only reads them through config.Secrets and never writes them.
package airole

import "slices"

// Providers a role can name. A later provider (issue #1081, the ChatGPT
// subscription) is one more constant here plus a case in Resolve.
const (
	// ProviderCustom is 独自 API: the Jev connection (JEV_API_KEY, JEV_BASE_URL)
	// and, for Sol/Opus/Luna, the role's own override (SOL_* etc.) as before.
	ProviderCustom = "custom"
	// ProviderOpenRouter is OpenRouter (OPENROUTER_API_KEY, OPENROUTER_BASE_URL).
	ProviderOpenRouter = "openrouter"
)

// DefaultOpenRouterBaseURL is OpenRouter's API root, used when
// OPENROUTER_BASE_URL is unset (the same value as trialwire.DefaultBaseURL; a
// test pins them equal).
const DefaultOpenRouterBaseURL = "https://openrouter.ai/api"

// Roles that call an AI.
const (
	// RoleJev is the live trader's judgement (Scout/Trader, what is traded).
	RoleJev = "jev"
	// RoleSol proposes threshold changes in the daily self-improvement.
	RoleSol = "sol"
	// RoleOpus reviews Sol's proposals.
	RoleOpus = "opus"
	// RoleLuna classifies news.
	RoleLuna = "luna"
)

// KeyPrefix starts every runtime_settings key of this package.
const KeyPrefix = "ai.role."

// Key is role's runtime_settings key.
func Key(role string) string { return KeyPrefix + role }

// IsKey reports whether key is one of this package's runtime_settings keys.
func IsKey(key string) bool {
	return len(key) > len(KeyPrefix) && key[:len(KeyPrefix)] == KeyPrefix && IsRole(key[len(KeyPrefix):])
}

// Info describes a role for the screens.
type Info struct {
	ID   string
	Name string
	// Description says what the role does.
	Description string
	// Live marks the role that decides what is traded: its provider and model
	// change from the Settings screen only.
	Live bool
}

var infos = []Info{
	{RoleJev, "Jev（売買判断）", "候補銘柄の評価と売買の判断を行います。取引に直接使われる役割です。", true},
	{RoleSol, "Sol（改善提案）", "日次の自己改善で、しきい値の変更案を作ります。", false},
	{RoleOpus, "Opus（提案の検証）", "Sol の改善提案を検証して承認・却下します。", false},
	{RoleLuna, "Luna（ニュース分類）", "ニュースを分類して、判断に渡す材料にします。", false},
}

// Infos returns every role in display order.
func Infos() []Info { return slices.Clone(infos) }

// Roles returns every role id in display order.
func Roles() []string {
	out := make([]string, len(infos))
	for i, in := range infos {
		out[i] = in.ID
	}
	return out
}

// IsRole reports whether role is a known role id.
func IsRole(role string) bool {
	for _, in := range infos {
		if in.ID == role {
			return true
		}
	}
	return false
}

// IsLive reports whether role is the live (traded) one.
func IsLive(role string) bool { return role == RoleJev }

// Providers returns the provider ids in display order.
func Providers() []string { return []string{ProviderCustom, ProviderOpenRouter} }

// IsProvider reports whether p is a known provider id.
func IsProvider(p string) bool { return p == ProviderCustom || p == ProviderOpenRouter }

// ProviderName is the Japanese name of a provider id (the id itself for an
// unknown one).
func ProviderName(p string) string {
	switch p {
	case ProviderCustom:
		return "独自 API"
	case ProviderOpenRouter:
		return "OpenRouter"
	}
	return p
}

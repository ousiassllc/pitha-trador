package config

import (
	"fmt"
	"os"
	"strings"
)

// Env* are the environment variables Secrets reads from. Unlike
// strategy.yaml/risk.yaml (checked into git, human-edited thresholds),
// these carry credentials and must never be committed
// (docs/environment/setup.md: `.env` is gitignored; `.env.example`
// documents the variable names only, with empty values).
const (
	// EnvJevAPIKey and EnvJevBaseURL supply internal/service/jev.Config's
	// APIKey/BaseURL fields (docs/architecture/overview.md §6).
	EnvJevAPIKey  = "JEV_API_KEY"
	EnvJevBaseURL = "JEV_BASE_URL"
	// EnvKabuAPIPassword supplies internal/service/marketdata.Config's
	// APIPassword field. It must match the APIPassword configured inside
	// the kabuステーションアプリ itself (docs/architecture/overview.md
	// §5).
	EnvKabuAPIPassword = "KABU_API_PASSWORD"
)

// Secrets holds every credential a composition-root sub-scope
// (issue #41's other split children, e.g. #44/#46) passes into
// internal/service/marketdata.Config and internal/service/jev.Config.
// Unlike StrategyConfig/RiskConfig (YAML files under config/, loaded by
// LoadStrategy/LoadRisk), Secrets is read from the process environment
// only - it has no on-disk representation of its own.
type Secrets struct {
	JevAPIKey       string
	JevBaseURL      string
	KabuAPIPassword string
}

// LoadSecrets reads Secrets from the process environment
// (JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD), returning an error
// naming every missing variable instead of returning a partially-empty
// Secrets. Silently falling back to an empty value (and thus a mock/
// disabled client further down the composition root) would leave a
// misconfigured deployment running with real trading logic silently
// skipped - issue #43's explicit non-scope note calls this out as the
// "非推奨" path deliberately not taken here. Callers that intentionally
// want to run without Jev/kabuステーションAPI (a future backtest-only
// entrypoint, or a unit test) should simply not call LoadSecrets rather
// than ignore its error.
func LoadSecrets() (Secrets, error) {
	s := Secrets{
		JevAPIKey:       os.Getenv(EnvJevAPIKey),
		JevBaseURL:      os.Getenv(EnvJevBaseURL),
		KabuAPIPassword: os.Getenv(EnvKabuAPIPassword),
	}

	var missing []string
	if s.JevAPIKey == "" {
		missing = append(missing, EnvJevAPIKey)
	}
	if s.JevBaseURL == "" {
		missing = append(missing, EnvJevBaseURL)
	}
	if s.KabuAPIPassword == "" {
		missing = append(missing, EnvKabuAPIPassword)
	}
	if len(missing) > 0 {
		return Secrets{}, fmt.Errorf("config: missing required environment variable(s): %s", strings.Join(missing, ", "))
	}
	return s, nil
}

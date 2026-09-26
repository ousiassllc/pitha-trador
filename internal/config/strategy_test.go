package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestLoadStrategy_ParsesRepositoryTemplateFile(t *testing.T) {
	cfg, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
	if err != nil {
		t.Fatalf("LoadStrategy(%q) returned error: %v", config.DefaultStrategyPath, err)
	}

	if got, want := cfg.Scan.FullScanIntervalSeconds, 60; got != want {
		t.Errorf("Scan.FullScanIntervalSeconds = %d, want %d", got, want)
	}
	if got, want := cfg.FastScreener.TopN, 20; got != want {
		t.Errorf("FastScreener.TopN = %d, want %d", got, want)
	}
	if got, want := cfg.JevScout.MinInterestingNow, 0.65; got != want {
		t.Errorf("JevScout.MinInterestingNow = %v, want %v", got, want)
	}
	if got, want := cfg.JevScout.MinLiquidityOk, 0.70; got != want {
		t.Errorf("JevScout.MinLiquidityOk = %v, want %v", got, want)
	}
	if got, want := cfg.JevScout.MinAbnormalActivity, 0.55; got != want {
		t.Errorf("JevScout.MinAbnormalActivity = %v, want %v", got, want)
	}
	if got, want := cfg.Policy.Long.MinEntryQuality, "strong"; got != want {
		t.Errorf("Policy.Long.MinEntryQuality = %q, want %q", got, want)
	}
}

func TestLoadStrategy_ReturnsErrorForMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	if _, err := config.LoadStrategy(missing); err == nil {
		t.Fatalf("LoadStrategy(%q) returned nil error, want error", missing)
	}
}

func TestLoadStrategy_ReturnsErrorForInvalidYAML(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "strategy.yaml")
	if err := os.WriteFile(invalid, []byte("scan: [this is not a mapping"), 0o600); err != nil {
		t.Fatalf("write invalid fixture: %v", err)
	}

	if _, err := config.LoadStrategy(invalid); err == nil {
		t.Fatalf("LoadStrategy(%q) returned nil error, want error", invalid)
	}
}

func TestLoadStrategy_AppliesPolicyEnvOverrides(t *testing.T) {
	t.Setenv("PITHA_POLICY_LONG_MIN_PROBABILITY", "0.75")
	t.Setenv("PITHA_POLICY_LONG_MIN_ENTRY_QUALITY", "exceptional")
	t.Setenv("PITHA_POLICY_SHORT_MAX_TOXIC_FLOW", "0.20")

	cfg, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
	if err != nil {
		t.Fatalf("LoadStrategy(%q) returned error: %v", config.DefaultStrategyPath, err)
	}

	if got, want := cfg.Policy.Long.MinProbability, 0.75; got != want {
		t.Errorf("Policy.Long.MinProbability = %v, want %v (PITHA_POLICY_LONG_MIN_PROBABILITY override)", got, want)
	}
	if got, want := cfg.Policy.Long.MinEntryQuality, "exceptional"; got != want {
		t.Errorf("Policy.Long.MinEntryQuality = %q, want %q (PITHA_POLICY_LONG_MIN_ENTRY_QUALITY override)", got, want)
	}
	if got, want := cfg.Policy.Short.MaxToxicFlow, 0.20; got != want {
		t.Errorf("Policy.Short.MaxToxicFlow = %v, want %v (PITHA_POLICY_SHORT_MAX_TOXIC_FLOW override)", got, want)
	}
	// An unset env var must not disturb the YAML-sourced value.
	if got, want := cfg.Policy.Short.MinProbability, 0.68; got != want {
		t.Errorf("Policy.Short.MinProbability = %v, want %v (no override set, YAML value must be kept)", got, want)
	}
}

func TestLoadStrategy_ReturnsErrorForInvalidPolicyEnvOverride(t *testing.T) {
	t.Setenv("PITHA_POLICY_LONG_MIN_PROBABILITY", "not-a-number")

	if _, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath)); err == nil {
		t.Fatalf("LoadStrategy(%q) returned nil error, want error for invalid PITHA_POLICY_LONG_MIN_PROBABILITY", config.DefaultStrategyPath)
	}
}

// repoPath resolves a path relative to the repository root, so tests keep
// working regardless of which package directory `go test` runs from.
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", rel)
}

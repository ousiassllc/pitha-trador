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
	if got, want := cfg.Scan.KabuInfoAPIMaxPerSecond, 8; got != want {
		t.Errorf("Scan.KabuInfoAPIMaxPerSecond = %d, want %d", got, want)
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
	if got, want := cfg.Scan.EventTrigger.VolumeRatioChangeThreshold, 2.0; got != want {
		t.Errorf("Scan.EventTrigger.VolumeRatioChangeThreshold = %v, want %v", got, want)
	}
	if got, want := cfg.Scan.EventTrigger.TradeFlowImbalanceChangeThreshold, 0.4; got != want {
		t.Errorf("Scan.EventTrigger.TradeFlowImbalanceChangeThreshold = %v, want %v", got, want)
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

// Issue #708: PITHA_POLICY_* / PITHA_FAST_SCREENER_* are no longer an
// override layer; the Settings screen (runtime_settings) replaces them.
func TestLoadStrategy_IgnoresRemovedEnvOverrides(t *testing.T) {
	t.Setenv("PITHA_POLICY_LONG_MIN_PROBABILITY", "0.75")
	t.Setenv("PITHA_POLICY_SHORT_MAX_TOXIC_FLOW", "not-a-number")
	t.Setenv("PITHA_FAST_SCREENER_MIN_PRICE", "250")
	t.Setenv("PITHA_FAST_SCREENER_TOP_N", "abc")

	cfg, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
	if err != nil {
		t.Fatalf("LoadStrategy(%q) returned error: %v", config.DefaultStrategyPath, err)
	}

	if got := cfg.Policy.Long.MinProbability; got == 0.75 {
		t.Errorf("Policy.Long.MinProbability = %v, want the YAML value (PITHA_POLICY_* is no longer read)", got)
	}
	if got := cfg.FastScreener.MinPrice; got == 250 {
		t.Errorf("FastScreener.MinPrice = %v, want the YAML value (PITHA_FAST_SCREENER_* is no longer read)", got)
	}
}

// repoPath resolves a path relative to the repository root, so tests keep
// working regardless of which package directory `go test` runs from.
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", rel)
}

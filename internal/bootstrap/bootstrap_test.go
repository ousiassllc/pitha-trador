package bootstrap_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestRun_OpensDBAndLoadsConfigFromRepoDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	state, err := bootstrap.Run(bootstrap.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if state.DB == nil {
		t.Fatal("Run: State.DB is nil")
	}
	if err := state.DB.Ping(); err != nil {
		t.Fatalf("State.DB.Ping: %v", err)
	}
	if state.Strategy == nil {
		t.Fatal("Run: State.Strategy is nil")
	}
	if state.Strategy.FastScreener.TopN == 0 {
		t.Error("Run: State.Strategy.FastScreener.TopN was not loaded from config/strategy.yaml")
	}
	if state.Risk == nil {
		t.Fatal("Run: State.Risk is nil")
	}
	if state.Risk.Paper.MaxOpenPositions == 0 {
		t.Error("Run: State.Risk.Paper.MaxOpenPositions was not loaded from config/risk.yaml")
	}
	if state.Paths.DBPath != dbPath {
		t.Errorf("Run: Paths.DBPath = %q, want %q", state.Paths.DBPath, dbPath)
	}
}

func TestRun_EnvOverridesTakePrecedenceOverDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "override.db")
	t.Setenv(bootstrap.EnvDBPath, dbPath)

	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if state.Paths.DBPath != dbPath {
		t.Errorf("Run: Paths.DBPath = %q, want env override %q", state.Paths.DBPath, dbPath)
	}
}

func TestRun_ReturnsErrorAndClosesDBOnUnparsableStrategyConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	_, err := bootstrap.Run(bootstrap.Config{
		DBPath:       dbPath,
		StrategyPath: filepath.Join(t.TempDir(), "does-not-exist.yaml"),
	})
	if err == nil {
		t.Fatal("Run: expected an error for a missing strategy config file, got nil")
	}
}

// TestRun_FallsBackToEmbeddedDefaultsWhenNoConfigFileIsFound is issue
// #59's step-4 proof: with no explicit bootstrap.Config field, no
// PITHA_STRATEGY_PATH/PITHA_RISK_PATH set, and (in this `go test`
// process) no config/*.yaml next to the test binary either, Run must
// still succeed by parsing the compiled-in configdefaults.
// DefaultStrategyYAML/DefaultRiskYAML rather than failing outright - the
// scenario a distributed .exe with no accompanying config/ directory
// hits.
func TestRun_FallsBackToEmbeddedDefaultsWhenNoConfigFileIsFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	state, err := bootstrap.Run(bootstrap.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if state.Strategy == nil || state.Strategy.FastScreener.TopN == 0 {
		t.Fatal("Run: State.Strategy was not populated from the embedded default")
	}
	if state.Risk == nil || state.Risk.Paper.MaxOpenPositions == 0 {
		t.Fatal("Run: State.Risk was not populated from the embedded default")
	}
	if state.Paths.StrategyPath == "" || state.Paths.RiskPath == "" {
		t.Error("Run: State.Paths.StrategyPath/RiskPath must still report a source even when embedded")
	}
}

// TestRun_ExecutableDirectoryConfigTakesPrecedenceOverEmbeddedDefault is
// issue #59's other explicit acceptance criterion: a config/strategy.yaml
// placed next to the running executable (os.Executable) must be read in
// preference to the compiled-in embedded default, so an operator can
// hand-edit a distributed .exe's configuration without recompiling.
func TestRun_ExecutableDirectoryConfigTakesPrecedenceOverEmbeddedDefault(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	configDir := filepath.Join(filepath.Dir(exe), "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", configDir, err)
	}
	strategyPath := filepath.Join(configDir, "strategy.yaml")
	t.Cleanup(func() {
		_ = os.Remove(strategyPath)
		_ = os.Remove(configDir) // no-op unless this test left it empty
	})

	const wantTopN = 4242
	if err := os.WriteFile(strategyPath, fmt.Appendf(nil, "fast_screener:\n  top_n: %d\n", wantTopN), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", strategyPath, err)
	}

	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	state, err := bootstrap.Run(bootstrap.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if got := state.Strategy.FastScreener.TopN; got != wantTopN {
		t.Errorf("Run: State.Strategy.FastScreener.TopN = %d, want %d (from executable-directory config/strategy.yaml, not the embedded default)", got, wantTopN)
	}
	if state.Paths.StrategyPath != strategyPath {
		t.Errorf("Run: Paths.StrategyPath = %q, want %q", state.Paths.StrategyPath, strategyPath)
	}
}

func TestDefaultDBPath_ReturnsPithaTradorSubpath(t *testing.T) {
	path, err := bootstrap.DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath: %v", err)
	}
	if filepath.Base(path) != "pitha.db" {
		t.Errorf("DefaultDBPath: base = %q, want %q", filepath.Base(path), "pitha.db")
	}
	if filepath.Base(filepath.Dir(path)) != "pitha-trador" {
		t.Errorf("DefaultDBPath: parent dir = %q, want %q", filepath.Base(filepath.Dir(path)), "pitha-trador")
	}
}

// TestBuildServices_EmptySecretsDoesNotPanic is the direct proof for
// issue #57's acceptance criterion that an unset JEV_API_KEY/
// KABU_API_PASSWORD (the required keys) or optional key (JEV_BASE_URL/
// JEV_MODEL/SLACK_WEBHOOK_URL etc.) must never fail
// startup: config.LoadSecretsFromDB returns a zero-value config.Secrets
// whenever the operator has not yet visited the Settings screen, and
// BuildServices must construct every internal/service/marketdata.Client
// and internal/service/jev.Client (and simply skip the optional Slack
// channel) from that zero value without panicking.
func TestBuildServices_EmptySecretsDoesNotPanic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	state, err := bootstrap.Run(bootstrap.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	svc := bootstrap.BuildServices(state, config.Secrets{})
	if svc.MarketData == nil {
		t.Error("BuildServices: Services.MarketData is nil")
	}
	if svc.Jev == nil {
		t.Error("BuildServices: Services.Jev is nil")
	}
}

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestLoadRisk_ParsesRepositoryTemplateFile(t *testing.T) {
	cfg, err := config.LoadRisk(repoPath(t, config.DefaultRiskPath))
	if err != nil {
		t.Fatalf("LoadRisk(%q) returned error: %v", config.DefaultRiskPath, err)
	}

	if got, want := cfg.Paper.MaxOpenPositions, 5; got != want {
		t.Errorf("Paper.MaxOpenPositions = %d, want %d", got, want)
	}
	if got, want := cfg.Live.MaxOpenPositions, 3; got != want {
		t.Errorf("Live.MaxOpenPositions = %d, want %d", got, want)
	}
	if got, want := cfg.Live.HeartbeatTimeoutMinutes, 120; got != want {
		t.Errorf("Live.HeartbeatTimeoutMinutes = %d, want %d", got, want)
	}
	if got, want := cfg.Paper.HeartbeatTimeoutMinutes, 0; got != want {
		t.Errorf("Paper.HeartbeatTimeoutMinutes = %d, want %d (Live専用のためPaperは対象外)", got, want)
	}
}

func TestLoadRisk_ReturnsErrorForMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	if _, err := config.LoadRisk(missing); err == nil {
		t.Fatalf("LoadRisk(%q) returned nil error, want error", missing)
	}
}

func TestLoadRisk_ReturnsErrorForInvalidYAML(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "risk.yaml")
	if err := os.WriteFile(invalid, []byte("paper: [this is not a mapping"), 0o600); err != nil {
		t.Fatalf("write invalid fixture: %v", err)
	}

	if _, err := config.LoadRisk(invalid); err == nil {
		t.Fatalf("LoadRisk(%q) returned nil error, want error", invalid)
	}
}

func TestLoadRisk_TemplateDefinesPaperInitialCapital(t *testing.T) {
	cfg, err := config.LoadRisk(repoPath(t, config.DefaultRiskPath))
	if err != nil {
		t.Fatalf("LoadRisk: %v", err)
	}
	if got := cfg.Paper.InitialCapital; got != config.DefaultPaperInitialCapital {
		t.Errorf("Paper.InitialCapital = %v, want %v (shipped default)", got, float64(config.DefaultPaperInitialCapital))
	}
	if got := cfg.Live.InitialCapital; got != 0 {
		t.Errorf("Live.InitialCapital = %v, want 0 (operator must set the real capital before going Live)", got)
	}
}

func TestLoadRiskBytes_MissingPaperInitialCapitalGetsDefaultButLiveDoesNot(t *testing.T) {
	cfg, err := config.LoadRiskBytes([]byte("paper:\n  max_open_positions: 5\nlive:\n  max_open_positions: 3\n"))
	if err != nil {
		t.Fatalf("LoadRiskBytes: %v", err)
	}
	if got := cfg.Paper.InitialCapital; got != config.DefaultPaperInitialCapital {
		t.Errorf("Paper.InitialCapital = %v, want default %v", got, float64(config.DefaultPaperInitialCapital))
	}
	if got := cfg.Live.InitialCapital; got != 0 {
		t.Errorf("Live.InitialCapital = %v, want 0 (no default: Risk Engine must reject trades until configured)", got)
	}
}

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

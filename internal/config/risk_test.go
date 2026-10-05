package config_test

import (
	"os"
	"path/filepath"
	"strings"
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
	if got, want := cfg.Paper.MaxSameDirectionPositions, 3; got != want {
		t.Errorf("Paper.MaxSameDirectionPositions = %d, want %d", got, want)
	}
	if got, want := cfg.Live.MarketAdverseReturn5mPct, 0.15; got != want {
		t.Errorf("Live.MarketAdverseReturn5mPct = %v, want %v", got, want)
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

// validRiskSection is a complete, valid limit set for one mode; tests
// start from it and break one key at a time.
const validRiskSection = `  max_position_per_symbol_pct: 2.0
  max_total_exposure_pct: 20.0
  max_daily_loss_pct: 1.0
  max_trade_loss_pct: 0.25
  max_open_positions: 5
  max_same_direction_positions: 3
  market_adverse_return_5m_pct: 0.2
  max_spread_bps: 30
  max_consecutive_losses: 4
  cooldown_after_loss_minutes: 5
  force_flat_before_market_close_minutes: 10
`

func TestLoadRiskBytes_MissingPaperInitialCapitalGetsDefaultButLiveDoesNot(t *testing.T) {
	cfg, err := config.LoadRiskBytes([]byte("paper:\n" + validRiskSection + "live:\n" + validRiskSection))
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

func TestLoadRiskBytes_RejectsInvalidLimitsNamingTheKey(t *testing.T) {
	tests := []struct {
		name, yaml, wantKey string
	}{
		{"paper max_daily_loss_pct 0", strings.Replace(validRiskSection, "max_daily_loss_pct: 1.0", "max_daily_loss_pct: 0", 1), "paper.max_daily_loss_pct"},
		{"paper max_trade_loss_pct negative", strings.Replace(validRiskSection, "max_trade_loss_pct: 0.25", "max_trade_loss_pct: -1", 1), "paper.max_trade_loss_pct"},
		{"paper max_consecutive_losses omitted", strings.Replace(validRiskSection, "  max_consecutive_losses: 4\n", "", 1), "paper.max_consecutive_losses"},
		{"paper max_open_positions 0", strings.Replace(validRiskSection, "max_open_positions: 5", "max_open_positions: 0", 1), "paper.max_open_positions"},
		{"paper max_same_direction_positions omitted", strings.Replace(validRiskSection, "  max_same_direction_positions: 3\n", "", 1), "paper.max_same_direction_positions"},
		{"paper market_adverse_return_5m_pct 0", strings.Replace(validRiskSection, "market_adverse_return_5m_pct: 0.2", "market_adverse_return_5m_pct: 0", 1), "paper.market_adverse_return_5m_pct"},
		{"paper max_spread_bps omitted", strings.Replace(validRiskSection, "  max_spread_bps: 30\n", "", 1), "paper.max_spread_bps"},
		{"paper max_position_per_symbol_pct nan", strings.Replace(validRiskSection, "max_position_per_symbol_pct: 2.0", "max_position_per_symbol_pct: .nan", 1), "paper.max_position_per_symbol_pct"},
		{"paper cooldown negative", strings.Replace(validRiskSection, "cooldown_after_loss_minutes: 5", "cooldown_after_loss_minutes: -1", 1), "paper.cooldown_after_loss_minutes"},
		{"paper force_flat negative", strings.Replace(validRiskSection, "force_flat_before_market_close_minutes: 10", "force_flat_before_market_close_minutes: -1", 1), "paper.force_flat_before_market_close_minutes"},
		{"paper heartbeat negative", validRiskSection + "  heartbeat_timeout_minutes: -1\n", "paper.heartbeat_timeout_minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadRiskBytes([]byte("paper:\n" + tt.yaml))
			if err == nil {
				t.Fatalf("LoadRiskBytes returned nil error, want error naming %q", tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("error %q does not name %q", err, tt.wantKey)
			}
		})
	}
}

func TestLoadRisk_RejectsInvalidLiveSectionButIgnoresAbsentOne(t *testing.T) {
	paper := "paper:\n" + validRiskSection

	if _, err := config.LoadRiskBytes([]byte(paper)); err != nil {
		t.Errorf("risk.yaml without a live section: error %v, want nil (Live not set up)", err)
	}

	live := "live:\n" + strings.Replace(validRiskSection, "max_daily_loss_pct: 1.0", "max_daily_loss_pct: 0", 1)
	path := filepath.Join(t.TempDir(), "risk.yaml")
	if err := os.WriteFile(path, []byte(paper+live), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := config.LoadRisk(path)
	if err == nil || !strings.Contains(err.Error(), "live.max_daily_loss_pct") {
		t.Errorf("LoadRisk error = %v, want one naming live.max_daily_loss_pct", err)
	}
}

func TestLoadRiskBytes_ReportsEveryViolationAtOnce(t *testing.T) {
	_, err := config.LoadRiskBytes([]byte("paper:\n  max_open_positions: 5\n"))
	if err == nil {
		t.Fatal("LoadRiskBytes returned nil error for a nearly empty paper section")
	}
	for _, key := range []string{"paper.max_daily_loss_pct", "paper.max_consecutive_losses"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name %q", err, key)
		}
	}
}

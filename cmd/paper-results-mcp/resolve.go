package main

import (
	"fmt"
	"os"
	"path/filepath"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
)

// resolveDBPath returns the database file the application opens
// (PITHA_DB_PATH, else bootstrap.DefaultDBPath). It is only opened read-only.
func resolveDBPath() (string, error) {
	if v := os.Getenv(bootstrap.EnvDBPath); v != "" {
		return v, nil
	}
	return bootstrap.DefaultDBPath()
}

// paperInitialCapital returns risk.yaml's paper.initial_capital with the
// application's resolution order: PITHA_RISK_PATH, config/risk.yaml next to
// the executable, then the compiled-in default. The Live capital is never
// read; an unset Paper value is config.DefaultPaperInitialCapital (applied by
// the loader).
func paperInitialCapital() (float64, error) {
	var (
		risk *config.RiskConfig
		err  error
	)
	if path := riskPath(); path != "" {
		risk, err = config.LoadRisk(path)
	} else {
		risk, err = config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	}
	if err != nil {
		return 0, fmt.Errorf("load risk config: %w", err)
	}
	return risk.Paper.InitialCapital, nil
}

func riskPath() string {
	if v := os.Getenv(bootstrap.EnvRiskPath); v != "" {
		return v
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), config.DefaultRiskPath)
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}

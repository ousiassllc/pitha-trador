package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// DefaultStrategyPath and DefaultRiskPath are the conventional locations of
// the strategy/risk configuration files relative to the repository root
// (docs/architecture/overview.md §3).
const (
	DefaultStrategyPath = "config/strategy.yaml"
	DefaultRiskPath     = "config/risk.yaml"
)

// loadYAMLFile reads the file at path and decodes it as YAML into a new
// value of type T.
func loadYAMLFile[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var v T
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	return &v, nil
}

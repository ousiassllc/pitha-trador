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

// loadYAMLBytes decodes data as YAML into a new value of type T. desc
// identifies the source (a file path, or a sentinel like "(embedded
// default)") for the wrapped error message only. Decoding is strict: a key
// the config struct does not know (typically a typo such as
// heartbeat_timout_minutes, which would otherwise silently leave the real
// setting at 0) is an error naming that key.
func loadYAMLBytes[T any](data []byte, desc string) (*T, error) {
	var v T
	if err := yaml.UnmarshalWithOptions(data, &v, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", desc, err)
	}
	return &v, nil
}

// loadYAMLFile reads the file at path and decodes it as YAML into a new
// value of type T.
func loadYAMLFile[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	return loadYAMLBytes[T](data, path)
}

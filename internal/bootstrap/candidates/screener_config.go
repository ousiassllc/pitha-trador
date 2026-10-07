package candidates

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// fastScreenerConfig returns the FR-FS-1/FR-FS-3 filter/weight settings
// for this cycle: r.Strategy.FastScreener (config/strategy.yaml, validated
// by config.LoadStrategy) overridden by every screener.* runtime_settings
// key in the DB - what the Settings screen edits (issue #708) - re-validated
// against FR-FS-4 when any applied. Read per cycle so a change takes effect
// on the next refresh without a restart.
func (r *Refresher) fastScreenerConfig(ctx context.Context) (config.FastScreenerConfig, error) {
	cfg := r.Strategy.FastScreener
	overridden := false
	for _, key := range config.FastScreenerSettingKeys() {
		raw, ok, err := r.Settings.Get(ctx, key)
		if err != nil {
			return config.FastScreenerConfig{}, fmt.Errorf("candidates: read runtime setting %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := config.ApplyFastScreenerSetting(&cfg, key, raw); err != nil {
			return config.FastScreenerConfig{}, fmt.Errorf("candidates: %w", err)
		}
		overridden = true
	}
	// The YAML base was validated by config.LoadStrategy; an override
	// layer must not break FR-FS-4's invariants either (a bad DB value would
	// silently disable the filters or empty the candidate list, issue #617).
	if overridden {
		if err := config.ValidateFastScreenerOverrides(cfg); err != nil {
			return config.FastScreenerConfig{}, fmt.Errorf("candidates: invalid screener.* runtime settings: %w", err)
		}
	}
	return cfg, nil
}

package entryqualitydiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// effectiveThresholds is the policy section of baseline (the build's default
// when nil) with the runtime_settings policy.* overrides in force applied,
// exactly how selfimprove.RuntimePolicy resolves them for the engine, except
// that min_entry_quality is left alone: each scenario sets its own. It also
// returns the keys that changed a value, sorted.
func effectiveThresholds(ctx context.Context, q query.Q, baseline *config.PolicyConfig) (config.PolicyConfig, []string, error) {
	var cfg config.PolicyConfig
	if baseline != nil {
		cfg = *baseline
	} else {
		strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
		if err != nil {
			return config.PolicyConfig{}, nil, fmt.Errorf("entryqualitydiag: load the default strategy: %w", err)
		}
		cfg = strategy.Policy
	}
	keys := make([]string, 0, len(domain.PolicyProposalKeys))
	for key := range domain.PolicyProposalKeys {
		if key != domain.PolicyKeyLongMinEntryQuality && key != domain.PolicyKeyShortMinEntryQuality {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	overridden := []string{}
	for _, key := range keys {
		var raw string
		err := q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return config.PolicyConfig{}, nil, fmt.Errorf("entryqualitydiag: read runtime setting %s: %w", key, err)
		}
		if err := config.ApplyPolicySetting(&cfg, key, raw); err != nil {
			return config.PolicyConfig{}, nil, fmt.Errorf("entryqualitydiag: apply runtime setting %s: %w", key, err)
		}
		overridden = append(overridden, key)
	}
	return cfg, overridden, nil
}

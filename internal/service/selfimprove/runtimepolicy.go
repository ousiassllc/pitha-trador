package selfimprove

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// RuntimePolicy is the read side of the policy.* runtime_settings keys
// Governor writes: config/strategy.yaml's PolicyConfig overridden by every
// applied proposal still in effect (FR-POLICY-4, FR-SELFIMPROVE-5). Live
// Policy Engine evaluation (internal/service/policy.WithPolicySource) and
// backtest/shadow-backtest RunConfigs read it, so an approved change
// reaches every consumer on its next read and a rollback likewise.
type RuntimePolicy struct {
	settings *repository.RuntimeSettingsRepository
	baseline config.PolicyConfig
}

// NewRuntimePolicy returns a RuntimePolicy over settings, falling back to
// baseline (config.LoadStrategy's PolicyConfig) for every policy.* key
// runtime_settings has no value for.
func NewRuntimePolicy(settings *repository.RuntimeSettingsRepository, baseline config.PolicyConfig) RuntimePolicy {
	return RuntimePolicy{settings: settings, baseline: baseline}
}

// CurrentThresholds returns the currently-active PolicyConfig.
func (p RuntimePolicy) CurrentThresholds(ctx context.Context) (config.PolicyConfig, error) {
	current := p.baseline
	for key := range domain.PolicyProposalKeys {
		raw, ok, err := p.settings.Get(ctx, key)
		if err != nil {
			return config.PolicyConfig{}, fmt.Errorf("selfimprove: read runtime setting %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := setPolicyField(&current, key, raw); err != nil {
			return config.PolicyConfig{}, err
		}
	}
	return current, nil
}

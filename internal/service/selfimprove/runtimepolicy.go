package selfimprove

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// RuntimePolicy is the read side of the policy.* runtime_settings keys
// Governor writes: config/strategy.yaml's PolicyConfig overridden by every
// applied proposal still in effect (FR-POLICY-4, FR-SELFIMPROVE-5). Live
// Policy Engine evaluation (internal/service/policy.WithPolicySource) and
// backtest/shadow-backtest RunConfigs read it, so an approved change
// reaches every consumer on its next read and a rollback likewise.
type RuntimePolicy struct {
	settings  *system.RuntimeSettingsRepository
	proposals *judgement.ProposalRepository
	baseline  config.PolicyConfig
}

// NewRuntimePolicy returns a RuntimePolicy over settings, falling back to
// baseline (config.LoadStrategy's PolicyConfig) for every policy.* key
// runtime_settings has no value for. proposals identifies the applied
// version in effect (AppliedPolicyVersion).
func NewRuntimePolicy(settings *system.RuntimeSettingsRepository, proposals *judgement.ProposalRepository, baseline config.PolicyConfig) RuntimePolicy {
	return RuntimePolicy{settings: settings, proposals: proposals, baseline: baseline}
}

// AppliedPolicyVersion returns the policy_version (e.g. "sol-12") of the
// applied proposal currently in effect: the status=applied proposal
// applied most recently (id breaks ties). A rolled-back proposal no longer
// counts, so after a rollback it is the previous still-applied proposal's
// version, or "" (the config/strategy.yaml baseline) when none remains.
func (p RuntimePolicy) AppliedPolicyVersion(ctx context.Context) (string, error) {
	applied, err := p.proposals.ListByStatus(ctx, domain.PolicyProposalStatusApplied)
	if err != nil {
		return "", fmt.Errorf("selfimprove: list applied proposals: %w", err)
	}
	var (
		latest  domain.PolicyProposal
		version string
	)
	for _, a := range applied {
		if a.AppliedPolicyVersion == nil || a.AppliedAt == nil {
			continue
		}
		if version != "" && (a.AppliedAt.Before(*latest.AppliedAt) || (a.AppliedAt.Equal(*latest.AppliedAt) && a.ID < latest.ID)) {
			continue
		}
		latest, version = a, *a.AppliedPolicyVersion
	}
	return version, nil
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

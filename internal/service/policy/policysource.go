package policy

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// PolicySource supplies the currently-active policy.* thresholds:
// config/strategy.yaml's values overridden by any runtime_settings
// policy.* key the Self-Improvement Governor has applied (FR-POLICY-4,
// FR-SELFIMPROVE-5). internal/service/selfimprove.RuntimePolicy
// implements it.
type PolicySource interface {
	CurrentThresholds(ctx context.Context) (config.PolicyConfig, error)
	// AppliedPolicyVersion returns the policy_version
	// (policy_proposals.applied_policy_version, e.g. "sol-12") of the
	// applied proposal currently in effect, or "" when none is - the
	// thresholds are then config/strategy.yaml's baseline.
	AppliedPolicyVersion(ctx context.Context) (string, error)
}

// signalPolicyVersion is the trade_signals.policy_version recorded for a
// live signal: Version alone on the baseline thresholds, or
// "<Version>+<applied_policy_version>" (e.g. "policy-v1+sol-12") while a
// Self-Improvement proposal's thresholds are in effect, so every signal
// identifies the applied version that produced it (FR-POLICY-5,
// FR-SELFIMPROVE-5/7). The column is VARCHAR(20): the suffix fits up to
// a six-digit proposal id.
func signalPolicyVersion(applied string) string {
	if applied == "" {
		return Version
	}
	return Version + "+" + applied
}

// Option configures an Engine beyond NewEngine's required dependencies.
type Option func(*Engine)

// WithPolicySource makes Evaluate read Thresholds.Policy from src on every
// call instead of using NewEngine's static thresholds.Policy, so an
// applied (or rolled-back) policy proposal takes effect on the very next
// live signal without a restart. Decide - the backtest replay path, which
// pins its own thresholds - is unaffected.
func WithPolicySource(src PolicySource) Option {
	return func(e *Engine) { e.policy = src }
}

// currentThresholds returns e.thresholds with Policy replaced by the
// PolicySource's current values when one is configured, together with the
// trade_signals.policy_version identifying them.
func (e *Engine) currentThresholds(ctx context.Context) (Thresholds, string, error) {
	if e.policy == nil {
		return e.thresholds, Version, nil
	}
	current, err := e.policy.CurrentThresholds(ctx)
	if err != nil {
		return Thresholds{}, "", fmt.Errorf("policy: read current policy thresholds: %w", err)
	}
	applied, err := e.policy.AppliedPolicyVersion(ctx)
	if err != nil {
		return Thresholds{}, "", fmt.Errorf("policy: read applied policy version: %w", err)
	}
	th := e.thresholds
	th.Policy = current
	return th, signalPolicyVersion(applied), nil
}

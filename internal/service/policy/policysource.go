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
// PolicySource's current values when one is configured.
func (e *Engine) currentThresholds(ctx context.Context) (Thresholds, error) {
	if e.policy == nil {
		return e.thresholds, nil
	}
	current, err := e.policy.CurrentThresholds(ctx)
	if err != nil {
		return Thresholds{}, fmt.Errorf("policy: read current policy thresholds: %w", err)
	}
	th := e.thresholds
	th.Policy = current
	return th, nil
}

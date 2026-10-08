package selfimprove

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// applyChangesToPolicyConfig returns current with every change's
// NewValue applied, without mutating current.
func applyChangesToPolicyConfig(current config.PolicyConfig, changes []domain.PolicyChange) (config.PolicyConfig, error) {
	candidate := current
	for _, c := range changes {
		if err := setPolicyField(&candidate, c.Key, c.NewValue); err != nil {
			return config.PolicyConfig{}, err
		}
	}
	return candidate, nil
}

// setPolicyField writes jsonValue (a JSON-encoded scalar, PolicyChange's
// own convention) into cfg's field named by key, one of
// domain.PolicyProposalKeys (config.PolicySettingKeys is the same set,
// shared with the Settings screen).
func setPolicyField(cfg *config.PolicyConfig, key, jsonValue string) error {
	if !domain.PolicyProposalKeys[key] {
		return fmt.Errorf("selfimprove: %q is not a policy.* threshold key (FR-SELFIMPROVE-2)", key)
	}
	if err := config.ApplyPolicySetting(cfg, key, jsonValue); err != nil {
		return fmt.Errorf("selfimprove: %w", err)
	}
	return nil
}

// policyFieldJSON returns cfg's current value for key as a JSON-encoded
// scalar (PolicyChange.OldValue's convention), or ok=false when key is not
// one of domain.PolicyProposalKeys.
func policyFieldJSON(cfg config.PolicyConfig, key string) (string, bool) {
	value, ok := config.PolicySettingValue(cfg, key)
	if !ok {
		return "", false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(data), true
}

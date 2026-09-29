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
// domain.PolicyProposalKeys.
func setPolicyField(cfg *config.PolicyConfig, key, jsonValue string) error {
	switch key {
	case domain.PolicyKeyLongMinProbability:
		return setFloatField(&cfg.Long.MinProbability, key, jsonValue)
	case domain.PolicyKeyLongMinEntryQuality:
		return setStringField(&cfg.Long.MinEntryQuality, key, jsonValue)
	case domain.PolicyKeyLongMinContinuationProbability:
		return setFloatField(&cfg.Long.MinContinuationProbability, key, jsonValue)
	case domain.PolicyKeyLongMaxToxicFlow:
		return setFloatField(&cfg.Long.MaxToxicFlow, key, jsonValue)
	case domain.PolicyKeyLongMaxLiquidityStressed:
		return setFloatField(&cfg.Long.MaxLiquidityStressed, key, jsonValue)

	case domain.PolicyKeyShortMinProbability:
		return setFloatField(&cfg.Short.MinProbability, key, jsonValue)
	case domain.PolicyKeyShortMinEntryQuality:
		return setStringField(&cfg.Short.MinEntryQuality, key, jsonValue)
	case domain.PolicyKeyShortMinContinuationProbability:
		return setFloatField(&cfg.Short.MinContinuationProbability, key, jsonValue)
	case domain.PolicyKeyShortMaxToxicFlow:
		return setFloatField(&cfg.Short.MaxToxicFlow, key, jsonValue)
	case domain.PolicyKeyShortMaxLiquidityStressed:
		return setFloatField(&cfg.Short.MaxLiquidityStressed, key, jsonValue)

	default:
		return fmt.Errorf("selfimprove: %q is not a policy.* threshold key (FR-SELFIMPROVE-2)", key)
	}
}

// policyFieldJSON returns cfg's current value for key as a JSON-encoded
// scalar (PolicyChange.OldValue's convention), or ok=false when key is not
// one of domain.PolicyProposalKeys.
func policyFieldJSON(cfg config.PolicyConfig, key string) (string, bool) {
	var value any
	switch key {
	case domain.PolicyKeyLongMinProbability:
		value = cfg.Long.MinProbability
	case domain.PolicyKeyLongMinEntryQuality:
		value = cfg.Long.MinEntryQuality
	case domain.PolicyKeyLongMinContinuationProbability:
		value = cfg.Long.MinContinuationProbability
	case domain.PolicyKeyLongMaxToxicFlow:
		value = cfg.Long.MaxToxicFlow
	case domain.PolicyKeyLongMaxLiquidityStressed:
		value = cfg.Long.MaxLiquidityStressed
	case domain.PolicyKeyShortMinProbability:
		value = cfg.Short.MinProbability
	case domain.PolicyKeyShortMinEntryQuality:
		value = cfg.Short.MinEntryQuality
	case domain.PolicyKeyShortMinContinuationProbability:
		value = cfg.Short.MinContinuationProbability
	case domain.PolicyKeyShortMaxToxicFlow:
		value = cfg.Short.MaxToxicFlow
	case domain.PolicyKeyShortMaxLiquidityStressed:
		value = cfg.Short.MaxLiquidityStressed
	default:
		return "", false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func setFloatField(dest *float64, key, jsonValue string) error {
	var v float64
	if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
		return fmt.Errorf("selfimprove: decode %s value %q as number: %w", key, jsonValue, err)
	}
	*dest = v
	return nil
}

func setStringField(dest *string, key, jsonValue string) error {
	var v string
	if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
		return fmt.Errorf("selfimprove: decode %s value %q as string: %w", key, jsonValue, err)
	}
	*dest = v
	return nil
}

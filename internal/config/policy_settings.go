package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

// policyField describes one FR-POLICY-4 tunable: the runtime_settings key
// (docs/architecture/er.md §runtime_settings, e.g.
// "policy.long.min_probability") and where its value lives in a
// PolicyConfig. Exactly one of float/str is non-nil.
type policyField struct {
	key   string
	float func(*PolicyConfig) *float64
	str   func(*PolicyConfig) *string
}

func policyDirectionFields(prefix string, dir func(*PolicyConfig) *PolicyDirectionThresholds) []policyField {
	return []policyField{
		{key: prefix + "min_probability", float: func(c *PolicyConfig) *float64 { return &dir(c).MinProbability }},
		{key: prefix + "min_entry_quality", str: func(c *PolicyConfig) *string { return &dir(c).MinEntryQuality }},
		{key: prefix + "min_continuation_probability", float: func(c *PolicyConfig) *float64 { return &dir(c).MinContinuationProbability }},
		{key: prefix + "max_toxic_flow", float: func(c *PolicyConfig) *float64 { return &dir(c).MaxToxicFlow }},
		{key: prefix + "max_liquidity_stressed", float: func(c *PolicyConfig) *float64 { return &dir(c).MaxLiquidityStressed }},
	}
}

var policyFields = append(
	policyDirectionFields("policy.long.", func(c *PolicyConfig) *PolicyDirectionThresholds { return &c.Long }),
	policyDirectionFields("policy.short.", func(c *PolicyConfig) *PolicyDirectionThresholds { return &c.Short })...,
)

// PolicySettingKeys returns every runtime_settings key ApplyPolicySetting
// accepts (FR-POLICY-4: the LONG/SHORT thresholds), in a stable order.
// internal/domain.PolicyProposalKeys is the same set.
func PolicySettingKeys() []string {
	keys := make([]string, len(policyFields))
	for i, f := range policyFields {
		keys[i] = f.key
	}
	return keys
}

// PolicySettingValue returns cfg's current value for key (one of
// PolicySettingKeys) - a float64, or a string for min_entry_quality - or
// ok=false when key is unknown.
func PolicySettingValue(cfg PolicyConfig, key string) (value any, ok bool) {
	for _, f := range policyFields {
		if f.key != key {
			continue
		}
		if f.float != nil {
			return *f.float(&cfg), true
		}
		return *f.str(&cfg), true
	}
	return nil, false
}

// ApplyPolicySetting writes jsonValue - a runtime_settings.value (er.md:
// JSON scalar: a number, or a string for min_entry_quality) - into cfg's
// field named by key, one of PolicySettingKeys (FR-POLICY-4: runtime_settings
// overrides YAML). It only decodes; range checks are
// ValidatePolicyOverrides's job.
func ApplyPolicySetting(cfg *PolicyConfig, key, jsonValue string) error {
	for _, f := range policyFields {
		if f.key != key {
			continue
		}
		if f.float != nil {
			var v float64
			if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
				return fmt.Errorf("config: decode runtime setting %s=%q as number: %w", key, jsonValue, err)
			}
			*f.float(cfg) = v
			return nil
		}
		var v string
		if err := json.Unmarshal([]byte(jsonValue), &v); err != nil {
			return fmt.Errorf("config: decode runtime setting %s=%q as string: %w", key, jsonValue, err)
		}
		*f.str(cfg) = v
		return nil
	}
	return fmt.Errorf("config: %q is not a policy runtime setting key", key)
}

// ValidatePolicyOverrides enforces the same invariants as LoadStrategy
// (every probability threshold in (0, 1], min_entry_quality one of the
// JevEntryQuality* values) on a PolicyConfig after runtime_settings
// policy.* overrides were applied to it, so the Settings screen cannot save
// a value that would fail open. Violations are named by their
// runtime_settings key ("policy.long.min_probability must be in (0, 1] ...")
// and joined.
func ValidatePolicyOverrides(cfg PolicyConfig) error {
	return errors.Join(cfg.validate("policy")...)
}

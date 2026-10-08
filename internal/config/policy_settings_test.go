package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestApplyPolicySetting_OverridesEveryKey(t *testing.T) {
	keys := config.PolicySettingKeys()
	if len(keys) != 10 {
		t.Fatalf("len(PolicySettingKeys) = %d, want 10 (5 per direction)", len(keys))
	}

	var cfg config.PolicyConfig
	for i, key := range keys {
		value := strconv.Itoa(i + 1)
		if strings.HasSuffix(key, ".min_entry_quality") {
			value = `"strong"`
		}
		if err := config.ApplyPolicySetting(&cfg, key, value); err != nil {
			t.Fatalf("ApplyPolicySetting(%q): %v", key, err)
		}
	}

	want := config.PolicyConfig{
		Long:  config.PolicyDirectionThresholds{MinProbability: 1, MinEntryQuality: "strong", MinContinuationProbability: 3, MaxToxicFlow: 4, MaxLiquidityStressed: 5},
		Short: config.PolicyDirectionThresholds{MinProbability: 6, MinEntryQuality: "strong", MinContinuationProbability: 8, MaxToxicFlow: 9, MaxLiquidityStressed: 10},
	}
	if cfg != want {
		t.Errorf("config after applying every key = %+v, want %+v", cfg, want)
	}
}

func TestApplyPolicySetting_RejectsBadInput(t *testing.T) {
	cases := map[string][2]string{
		"unknown key":             {"policy.long.nope", "0.5"},
		"non-numeric probability": {"policy.long.min_probability", `"abc"`},
		"numeric entry quality":   {"policy.short.min_entry_quality", "3"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			var cfg config.PolicyConfig
			if err := config.ApplyPolicySetting(&cfg, kv[0], kv[1]); err == nil {
				t.Errorf("ApplyPolicySetting(%q, %q) = nil, want error", kv[0], kv[1])
			}
		})
	}
}

func TestValidatePolicyOverrides(t *testing.T) {
	valid := config.PolicyDirectionThresholds{MinProbability: 0.7, MinEntryQuality: "good", MinContinuationProbability: 0.6, MaxToxicFlow: 0.3, MaxLiquidityStressed: 0.3}
	if err := config.ValidatePolicyOverrides(config.PolicyConfig{Long: valid, Short: valid}); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	bad := valid
	bad.MinProbability = 1.5
	bad.MinEntryQuality = "stong"
	err := config.ValidatePolicyOverrides(config.PolicyConfig{Long: valid, Short: bad})
	if err == nil {
		t.Fatal("invalid policy accepted")
	}
	for _, want := range []string{"policy.short.min_probability", "policy.short.min_entry_quality"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestPolicySettingValue_ReadsEveryKey(t *testing.T) {
	cfg := config.PolicyConfig{
		Long:  config.PolicyDirectionThresholds{MinProbability: 0.1, MinEntryQuality: "good", MinContinuationProbability: 0.3, MaxToxicFlow: 0.4, MaxLiquidityStressed: 0.5},
		Short: config.PolicyDirectionThresholds{MinProbability: 0.6, MinEntryQuality: "strong", MinContinuationProbability: 0.8, MaxToxicFlow: 0.9, MaxLiquidityStressed: 1},
	}
	want := []any{0.1, "good", 0.3, 0.4, 0.5, 0.6, "strong", 0.8, 0.9, 1.0}
	for i, key := range config.PolicySettingKeys() {
		got, ok := config.PolicySettingValue(cfg, key)
		if !ok || got != want[i] {
			t.Errorf("PolicySettingValue(%q) = %v, %v, want %v, true", key, got, ok, want[i])
		}
	}
	if _, ok := config.PolicySettingValue(cfg, "policy.long.nope"); ok {
		t.Error("PolicySettingValue(unknown key) ok = true, want false")
	}
}

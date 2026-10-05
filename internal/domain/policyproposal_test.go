package domain_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestValidatePolicyChanges_RejectsRiskAndPromptVersionKeys(t *testing.T) {
	cases := []string{
		"risk.max_position_size",
		"risk.daily_loss_limit_jpy",
		"prompt_version",
		"jev.prompt_version",
		"policy.long.unknown_field",
	}
	for _, key := range cases {
		changes := []domain.PolicyChange{{Key: key, OldValue: `0.6`, NewValue: `0.62`}}
		if err := domain.ValidatePolicyChanges(changes); err == nil {
			t.Fatalf("ValidatePolicyChanges(key=%q) = nil, want error (FR-SELFIMPROVE-2)", key)
		}
	}
}

func TestValidatePolicyChanges_RejectsEmptyChanges(t *testing.T) {
	if err := domain.ValidatePolicyChanges(nil); err == nil {
		t.Fatalf("ValidatePolicyChanges(nil) = nil, want error")
	}
}

func TestValidatePolicyChanges_AcceptsConfidenceStepWithinCap(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		t.Fatalf("ValidatePolicyChanges(+0.05) = %v, want nil", err)
	}
}

func TestValidatePolicyChanges_RejectsConfidenceStepExceedingCap(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.66`},
	}
	if err := domain.ValidatePolicyChanges(changes); err == nil {
		t.Fatalf("ValidatePolicyChanges(+0.06) = nil, want error (FR-SELFIMPROVE-3 ±0.05 cap)")
	}
}

func TestValidatePolicyChanges_AcceptsEntryQualityOneStep(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinEntryQuality, OldValue: `"good"`, NewValue: `"strong"`},
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		t.Fatalf("ValidatePolicyChanges(good->strong) = %v, want nil", err)
	}
}

func TestValidatePolicyChanges_RejectsEntryQualityTwoSteps(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyShortMinEntryQuality, OldValue: `"fair"`, NewValue: `"strong"`},
	}
	if err := domain.ValidatePolicyChanges(changes); err == nil {
		t.Fatalf("ValidatePolicyChanges(fair->strong, 2 steps) = nil, want error (FR-SELFIMPROVE-3 1-step cap)")
	}
}

func TestValidatePolicyChanges_RejectsUnrecognizedEntryQualityValue(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinEntryQuality, OldValue: `"good"`, NewValue: `"amazing"`},
	}
	if err := domain.ValidatePolicyChanges(changes); err == nil {
		t.Fatalf("ValidatePolicyChanges(unrecognized entry_quality) = nil, want error")
	}
}

func TestParseAndEncodePolicyChanges_RoundTrip(t *testing.T) {
	changes := []domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	}
	encoded, err := domain.EncodePolicyChanges(changes)
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	decoded, err := domain.ParsePolicyChanges(encoded)
	if err != nil {
		t.Fatalf("ParsePolicyChanges: %v", err)
	}
	if len(decoded) != 1 || decoded[0] != changes[0] {
		t.Fatalf("ParsePolicyChanges(EncodePolicyChanges(x)) = %+v, want %+v", decoded, changes)
	}
}

func TestEntryQualityRank_OrdersWorstToBest(t *testing.T) {
	poor, _ := domain.EntryQualityRank(domain.JevEntryQualityPoor)
	exceptional, _ := domain.EntryQualityRank(domain.JevEntryQualityExceptional)
	if poor != 0 || exceptional != 4 {
		t.Fatalf("EntryQualityRank(poor)=%d, EntryQualityRank(exceptional)=%d, want 0 and 4", poor, exceptional)
	}
	if _, ok := domain.EntryQualityRank("unknown"); ok {
		t.Fatalf("EntryQualityRank(unknown) ok = true, want false")
	}
}

func TestValidatePolicyChanges_RejectsOutOfRangeConfidenceAndDuplicateKeys(t *testing.T) {
	tests := map[string][]domain.PolicyChange{
		"above 1": {{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.98", NewValue: "1.02"}},
		"below 0": {{Key: domain.PolicyKeyLongMaxToxicFlow, OldValue: "0.02", NewValue: "-0.02"}},
		"duplicate key": {
			{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.62"},
			{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.64"},
		},
	}
	for name, changes := range tests {
		t.Run(name, func(t *testing.T) {
			if err := domain.ValidatePolicyChanges(changes); err == nil {
				t.Errorf("ValidatePolicyChanges(%+v) = nil, want an error", changes)
			}
		})
	}
}

func TestValidatePolicyChanges_ConfidenceRangeMatchesConfigThresholds(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		wantErr  bool
	}{
		{"zero rejected", `0.05`, `0`, true},
		{"negative rejected", `0.02`, `-0.01`, true},
		{"above one rejected", `0.98`, `1.03`, true},
		{"one allowed", `0.96`, `1`, false},
		{"small positive allowed", `0.05`, `0.01`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changes := []domain.PolicyChange{{Key: domain.PolicyKeyLongMaxToxicFlow, OldValue: tc.old, NewValue: tc.new}}
			err := domain.ValidatePolicyChanges(changes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePolicyChanges(%s -> %s) error = %v, wantErr %v", tc.old, tc.new, err, tc.wantErr)
			}
		})
	}
}

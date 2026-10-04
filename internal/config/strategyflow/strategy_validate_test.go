package strategyflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// validStrategySections is a strategy.yaml body (everything except scan:)
// that passes StrategyConfig.Validate. Tests that exercise scan.* append it
// to their scan fixture; validation tests mutate it via strategyYAML.
const validStrategySections = `
fast_screener:
  min_price: 100
  max_price: 500000
  min_turnover_5m_jpy: 10000000
  max_spread_bps: 50
  min_volume_ratio: 1.2
  min_abs_return_5m_pct: 0.3
  min_realized_volatility: 0.001
  top_n: 20
  weights:
    volume_ratio: 0.2
    abs_return_5m: 0.2
    breakout_strength: 0.2
    orderbook_imbalance: 0.2
    volatility_expansion: 0.2
jev_scout:
  min_interesting_now: 0.65
  min_liquidity_ok: 0.70
  min_abnormal_activity: 0.55
policy:
  min_calibration_samples: 20
  long:
    min_probability: 0.68
    min_entry_quality: strong
    min_continuation_probability: 0.60
    max_toxic_flow: 0.35
    max_liquidity_stressed: 0.25
  short:
    min_probability: 0.68
    min_entry_quality: strong
    min_continuation_probability: 0.60
    max_toxic_flow: 0.35
    max_liquidity_stressed: 0.25
`

// strategyYAML returns validStrategySections with every "old" -> "new"
// replacement applied; it fails the test when an old string is absent so a
// typo in a case cannot silently test the valid config.
func strategyYAML(t *testing.T, replacements ...string) []byte {
	t.Helper()
	s := validStrategySections
	for i := 0; i < len(replacements); i += 2 {
		if !strings.Contains(s, replacements[i]) {
			t.Fatalf("fixture has no %q to replace", replacements[i])
		}
		s = strings.Replace(s, replacements[i], replacements[i+1], 1)
	}
	return []byte(s)
}

func TestLoadStrategyBytes_AcceptsValidFixture(t *testing.T) {
	if _, err := config.LoadStrategyBytes(strategyYAML(t)); err != nil {
		t.Fatalf("LoadStrategyBytes(valid fixture) returned error: %v", err)
	}
}

func TestLoadStrategyBytes_RejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		yaml    []byte
		wantKey string // substring of the error: the offending YAML path
	}{
		{"top_n negative", strategyYAML(t, "top_n: 20", "top_n: -1"), "fast_screener.top_n"},
		{"top_n zero", strategyYAML(t, "top_n: 20", "top_n: 0"), "fast_screener.top_n"},
		{"top_n missing", strategyYAML(t, "  top_n: 20\n", ""), "fast_screener.top_n"},
		{"fast_screener section missing", []byte("jev_scout:\n  min_interesting_now: 0.65\n  min_liquidity_ok: 0.7\n  min_abnormal_activity: 0.55\n"), "fast_screener.min_price"},
		{"min_price zero", strategyYAML(t, "min_price: 100", "min_price: 0"), "fast_screener.min_price"},
		{"max_price below min_price", strategyYAML(t, "max_price: 500000", "max_price: 50"), "fast_screener.max_price"},
		{"max_spread_bps negative", strategyYAML(t, "max_spread_bps: 50", "max_spread_bps: -5"), "fast_screener.max_spread_bps"},
		{"min_turnover missing", strategyYAML(t, "  min_turnover_5m_jpy: 10000000\n", ""), "fast_screener.min_turnover_5m_jpy"},
		{"min_volume_ratio zero", strategyYAML(t, "min_volume_ratio: 1.2", "min_volume_ratio: 0"), "fast_screener.min_volume_ratio"},
		{"weight negative", strategyYAML(t, "volume_ratio: 0.2\n    abs", "volume_ratio: -0.2\n    abs"), "fast_screener.weights.volume_ratio"},
		{"weights all missing", strategyYAML(t,
			"  weights:\n    volume_ratio: 0.2\n    abs_return_5m: 0.2\n    breakout_strength: 0.2\n    orderbook_imbalance: 0.2\n    volatility_expansion: 0.2\n", ""),
			"fast_screener.weights"},
		{"jev_scout.min_interesting_now missing", strategyYAML(t, "  min_interesting_now: 0.65\n", ""), "jev_scout.min_interesting_now"},
		{"jev_scout.min_liquidity_ok zero", strategyYAML(t, "min_liquidity_ok: 0.70", "min_liquidity_ok: 0"), "jev_scout.min_liquidity_ok"},
		{"jev_scout.min_abnormal_activity above 1", strategyYAML(t, "min_abnormal_activity: 0.55", "min_abnormal_activity: 1.5"), "jev_scout.min_abnormal_activity"},
		{"policy section missing", []byte(strings.SplitAfter(string(strategyYAML(t)), "policy:")[0]), "policy.long.min_probability"},
		{"policy.long.min_probability missing", strategyYAML(t, "    min_probability: 0.68\n    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0.35\n    max_liquidity_stressed: 0.25\n  short:",
			"    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0.35\n    max_liquidity_stressed: 0.25\n  short:"), "policy.long.min_probability"},
		{"policy.long.min_probability above 1", strategyYAML(t, "min_probability: 0.68", "min_probability: 68"), "policy.long.min_probability"},
		{"policy.short.max_toxic_flow zero", strategyYAML(t, "  short:\n    min_probability: 0.68\n    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0.35",
			"  short:\n    min_probability: 0.68\n    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0"), "policy.short.max_toxic_flow"},
		{"policy.short.max_liquidity_stressed negative", strategyYAML(t, "  short:\n    min_probability: 0.68\n    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0.35\n    max_liquidity_stressed: 0.25",
			"  short:\n    min_probability: 0.68\n    min_entry_quality: strong\n    min_continuation_probability: 0.60\n    max_toxic_flow: 0.35\n    max_liquidity_stressed: -0.25"), "policy.short.max_liquidity_stressed"},
		{"min_entry_quality typo", strategyYAML(t, "min_entry_quality: strong", "min_entry_quality: stong"), "policy.long.min_entry_quality"},
		{"min_entry_quality empty", strategyYAML(t, "min_entry_quality: strong", `min_entry_quality: ""`), "policy.long.min_entry_quality"},
		{"min_calibration_samples negative", strategyYAML(t, "min_calibration_samples: 20", "min_calibration_samples: -1"), "policy.min_calibration_samples"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.LoadStrategyBytes(tc.yaml)
			if err == nil {
				t.Fatalf("LoadStrategyBytes returned nil error, want error naming %q", tc.wantKey)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error = %q, want it to name %q", err, tc.wantKey)
			}
		})
	}
}

func TestLoadStrategyBytes_AcceptsBoundaryValues(t *testing.T) {
	yaml := strategyYAML(t,
		"top_n: 20", "top_n: 1",
		"min_calibration_samples: 20", "min_calibration_samples: 0",
		"min_interesting_now: 0.65", "min_interesting_now: 1",
		"max_price: 500000", "max_price: 100", // == min_price
		"min_entry_quality: strong", "min_entry_quality: poor",
	)
	if _, err := config.LoadStrategyBytes(yaml); err != nil {
		t.Fatalf("LoadStrategyBytes(boundary values) returned error: %v", err)
	}
}

// Every violation is reported in one error, like risk.yaml's Validate.
func TestLoadStrategyBytes_ReportsAllViolations(t *testing.T) {
	_, err := config.LoadStrategyBytes(strategyYAML(t,
		"top_n: 20", "top_n: -1",
		"min_liquidity_ok: 0.70", "min_liquidity_ok: 0",
		"min_entry_quality: strong", "min_entry_quality: bogus",
	))
	if err == nil {
		t.Fatal("LoadStrategyBytes returned nil error, want error")
	}
	for _, key := range []string{"fast_screener.top_n", "jev_scout.min_liquidity_ok", "policy.long.min_entry_quality"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error = %q, want it to name %q", err, key)
		}
	}
}

// LoadStrategy (file) must validate too, not just LoadStrategyBytes.
func TestLoadStrategy_RejectsInvalidFile(t *testing.T) {
	path := t.TempDir() + "/strategy.yaml"
	if err := os.WriteFile(path, strategyYAML(t, "top_n: 20", "top_n: -1"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := config.LoadStrategy(path)
	if err == nil || !strings.Contains(err.Error(), "fast_screener.top_n") {
		t.Fatalf("LoadStrategy(top_n: -1) error = %v, want one naming fast_screener.top_n", err)
	}
}

// Env overrides are applied before validation, so an out-of-range value
// that arrives via PITHA_* must be rejected like one from the YAML.
func TestLoadStrategy_RejectsInvalidEnvOverrides(t *testing.T) {
	tests := []struct {
		env, value, wantKey string
	}{
		{"PITHA_POLICY_LONG_MIN_PROBABILITY", "0", "policy.long.min_probability"},
		{"PITHA_POLICY_LONG_MIN_PROBABILITY", "1.5", "policy.long.min_probability"},
		{"PITHA_POLICY_LONG_MIN_PROBABILITY", "NaN", "policy.long.min_probability"},
		{"PITHA_POLICY_SHORT_MIN_ENTRY_QUALITY", "stong", "policy.short.min_entry_quality"},
		{"PITHA_POLICY_SHORT_MIN_ENTRY_QUALITY", "", "policy.short.min_entry_quality"},
		{"PITHA_POLICY_LONG_MIN_CONTINUATION_PROBABILITY", "-0.1", "policy.long.min_continuation_probability"},
		{"PITHA_POLICY_SHORT_MAX_TOXIC_FLOW", "0", "policy.short.max_toxic_flow"},
		{"PITHA_POLICY_LONG_MAX_LIQUIDITY_STRESSED", "2", "policy.long.max_liquidity_stressed"},
		{"PITHA_FAST_SCREENER_TOP_N", "0", "fast_screener.top_n"},
		{"PITHA_FAST_SCREENER_MAX_PRICE", "50", "fast_screener.max_price"},
	}
	for _, tc := range tests {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			_, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
			if err == nil || !strings.Contains(err.Error(), tc.wantKey) {
				t.Fatalf("LoadStrategy with %s=%q error = %v, want one naming %q", tc.env, tc.value, err, tc.wantKey)
			}
		})
	}
}

// config must not import internal/domain outside tests (doc.go), so its
// entry-quality list is a copy; it has to cover exactly the domain values.
func TestEntryQualityValues_MatchDomain(t *testing.T) {
	for _, q := range []string{
		domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
		domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
	} {
		yaml := strategyYAML(t, "min_entry_quality: strong", "min_entry_quality: "+q)
		if _, err := config.LoadStrategyBytes(yaml); err != nil {
			t.Errorf("min_entry_quality %q rejected: %v", q, err)
		}
	}
}

// repoPath resolves a path relative to the repository root, so tests keep
// working regardless of which package directory `go test` runs from.
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", "..", rel)
}

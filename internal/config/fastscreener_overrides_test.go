package config_test

import (
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestApplyFastScreenerSetting_OverridesEveryDocumentedKey(t *testing.T) {
	keys := config.FastScreenerSettingKeys()
	if len(keys) != 13 {
		t.Fatalf("len(FastScreenerSettingKeys) = %d, want 13 (7 filters + top_n + 5 weights)", len(keys))
	}

	var cfg config.FastScreenerConfig
	for i, key := range keys {
		// Distinct non-zero integers so a key wired to the wrong field
		// shows up as a mismatch below.
		if err := config.ApplyFastScreenerSetting(&cfg, key, strconv.Itoa(i+1)); err != nil {
			t.Fatalf("ApplyFastScreenerSetting(%q): %v", key, err)
		}
	}

	want := config.FastScreenerConfig{
		MinPrice: 1, MaxPrice: 2, MinTurnover5mJPY: 3, MaxSpreadBps: 4,
		MinVolumeRatio: 5, MinAbsReturn5mPct: 6, MinRealizedVolatility: 7,
		TopN: 8,
		Weights: config.FastScreenerWeights{
			VolumeRatio: 9, AbsReturn5m: 10, BreakoutStrength: 11,
			OrderbookImbalance: 12, VolatilityExpansion: 13,
		},
	}
	if cfg != want {
		t.Errorf("config after applying every key = %+v, want %+v", cfg, want)
	}
}

func TestApplyFastScreenerSetting_RejectsBadInput(t *testing.T) {
	cases := map[string][2]string{
		"unknown key":      {"screener.nope", "1"},
		"non-numeric JSON": {"screener.min_price", `"abc"`},
		"fractional top_n": {"screener.top_n", "2.5"},
		"negative top_n":   {"screener.top_n", "-3"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			var cfg config.FastScreenerConfig
			if err := config.ApplyFastScreenerSetting(&cfg, kv[0], kv[1]); err == nil {
				t.Errorf("ApplyFastScreenerSetting(%q, %q) = nil, want error", kv[0], kv[1])
			}
		})
	}
}

func TestFastScreenerSettingValue_ReadsEveryKey(t *testing.T) {
	cfg := config.FastScreenerConfig{
		MinPrice: 1, MaxPrice: 2, MinTurnover5mJPY: 3, MaxSpreadBps: 4,
		MinVolumeRatio: 5, MinAbsReturn5mPct: 6, MinRealizedVolatility: 7,
		TopN: 8,
		Weights: config.FastScreenerWeights{
			VolumeRatio: 9, AbsReturn5m: 10, BreakoutStrength: 11,
			OrderbookImbalance: 12, VolatilityExpansion: 13,
		},
	}
	for i, key := range config.FastScreenerSettingKeys() {
		got, ok := config.FastScreenerSettingValue(cfg, key)
		if !ok || got != float64(i+1) {
			t.Errorf("FastScreenerSettingValue(%q) = %v, %v, want %d, true", key, got, ok, i+1)
		}
	}
	if _, ok := config.FastScreenerSettingValue(cfg, "screener.nope"); ok {
		t.Error("FastScreenerSettingValue(unknown key) ok = true, want false")
	}
}

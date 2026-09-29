package config_test

import (
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestLoadStrategy_AppliesFastScreenerEnvOverrides(t *testing.T) {
	t.Setenv("PITHA_FAST_SCREENER_MIN_PRICE", "250")
	t.Setenv("PITHA_FAST_SCREENER_TOP_N", "35")
	t.Setenv("PITHA_FAST_SCREENER_WEIGHT_BREAKOUT_STRENGTH", "0.5")
	t.Setenv("PITHA_FAST_SCREENER_WEIGHT_VOLATILITY_EXPANSION", "0.05")

	cfg, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
	if err != nil {
		t.Fatalf("LoadStrategy: %v", err)
	}

	fs := cfg.FastScreener
	if fs.MinPrice != 250 {
		t.Errorf("MinPrice = %v, want 250 (env override)", fs.MinPrice)
	}
	if fs.TopN != 35 {
		t.Errorf("TopN = %d, want 35 (env override)", fs.TopN)
	}
	if fs.Weights.BreakoutStrength != 0.5 || fs.Weights.VolatilityExpansion != 0.05 {
		t.Errorf("Weights breakout/volatility = %v/%v, want 0.5/0.05 (env override)", fs.Weights.BreakoutStrength, fs.Weights.VolatilityExpansion)
	}
	// Unset variables keep the YAML value.
	if fs.MaxPrice != 500000 {
		t.Errorf("MaxPrice = %v, want 500000 (no override set, YAML value must be kept)", fs.MaxPrice)
	}
	if fs.Weights.VolumeRatio != 0.2 {
		t.Errorf("Weights.VolumeRatio = %v, want 0.2 (YAML value)", fs.Weights.VolumeRatio)
	}
}

func TestLoadStrategy_RejectsInvalidFastScreenerEnvOverride(t *testing.T) {
	for name, value := range map[string]string{
		"not a number":     "abc",
		"fractional top_n": "2.5",
		"negative top_n":   "-1",
	} {
		t.Run(name, func(t *testing.T) {
			key := "PITHA_FAST_SCREENER_MIN_PRICE"
			if name != "not a number" {
				key = "PITHA_FAST_SCREENER_TOP_N"
			}
			t.Setenv(key, value)

			if _, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath)); err == nil {
				t.Fatalf("LoadStrategy with %s=%q returned nil error", key, value)
			}
		})
	}
}

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

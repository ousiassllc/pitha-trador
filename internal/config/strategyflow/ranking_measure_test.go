package strategyflow_test

import (
	"reflect"
	"strings"
	"testing"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	internalconfig "github.com/ousiassllc/pitha-trador/internal/config"
)

func TestLoadStrategyBytes_FullScanEnabled(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{"omitted means off", "", false},
		{"explicit true turns the full scan on", "scan:\n  full_scan_enabled: true\n", true},
		{"explicit false turns the full scan off", "scan:\n  full_scan_enabled: false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := internalconfig.LoadStrategyBytes([]byte(tc.yaml + validStrategySections))
			if err != nil {
				t.Fatalf("LoadStrategyBytes: %v", err)
			}
			if got := cfg.Scan.FullScanOn(); got != tc.want {
				t.Errorf("FullScanOn() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadStrategyBytes_RankingMeasureIsOffByDefaultAndFilled(t *testing.T) {
	cfg, err := internalconfig.LoadStrategyBytes([]byte(validStrategySections))
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	want := internalconfig.RankingMeasureConfig{
		Enabled:         false,
		IntervalSeconds: internalconfig.DefaultRankingMeasureIntervalSeconds,
		Types:           []int{1, 2, 3, 4, 5, 6, 7},
		Exchanges:       []string{"T", "TP", "TS", "TG"},
	}
	if !reflect.DeepEqual(cfg.Scan.RankingMeasure, want) {
		t.Errorf("RankingMeasure = %+v, want %+v", cfg.Scan.RankingMeasure, want)
	}
}

func TestLoadStrategyBytes_RankingMeasureExplicitValues(t *testing.T) {
	yaml := "scan:\n  ranking_measure:\n    enabled: true\n    include_outside_session: true\n" +
		"    interval_seconds: 15\n    types: [1, 6]\n    exchanges: [T, ALL]\n"
	cfg, err := internalconfig.LoadStrategyBytes([]byte(yaml + validStrategySections))
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	want := internalconfig.RankingMeasureConfig{
		Enabled: true, IncludeOutsideSession: true, IntervalSeconds: 15,
		Types: []int{1, 6}, Exchanges: []string{"T", "ALL"},
	}
	if !reflect.DeepEqual(cfg.Scan.RankingMeasure, want) {
		t.Errorf("RankingMeasure = %+v, want %+v", cfg.Scan.RankingMeasure, want)
	}
}

func TestLoadStrategyBytes_RejectsInvalidRankingMeasure(t *testing.T) {
	cases := []struct{ name, yaml, wantErr string }{
		{"type 0", "    types: [0]\n", "scan.ranking_measure.types"},
		{"type 16", "    types: [16]\n", "scan.ranking_measure.types"},
		{"unknown exchange", "    exchanges: [XX]\n", "scan.ranking_measure.exchanges"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := "scan:\n  ranking_measure:\n" + tc.yaml
			_, err := internalconfig.LoadStrategyBytes([]byte(yaml + validStrategySections))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// The shipped strategy.yaml keeps the measurement opt-in and the full scan off.
func TestDefaultStrategyYAML_RankingMeasureAndFullScanOff(t *testing.T) {
	cfg, err := internalconfig.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	if cfg.Scan.RankingMeasure.Enabled {
		t.Error("shipped strategy.yaml enables scan.ranking_measure; it must be opt-in")
	}
	if cfg.Scan.FullScanOn() {
		t.Error("shipped strategy.yaml turns the full scan on; the ranking-based watch is the default")
	}
}

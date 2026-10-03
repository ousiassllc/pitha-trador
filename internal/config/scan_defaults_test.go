package config_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// A strategy.yaml predating (or zeroing) the scan.*_seconds keys must not
// leave a 0 interval, which would spin the candidate refresh loop with no
// wait and schedule "@every 0s" full scans.
func TestLoadStrategyBytes_FillsUnusableScanIntervals(t *testing.T) {
	defaults := config.ScanConfig{
		FullScanIntervalSeconds:            config.DefaultFullScanIntervalSeconds,
		CandidateRefreshIntervalSecondsMin: config.DefaultCandidateRefreshIntervalSecondsMin,
		CandidateRefreshIntervalSecondsMax: config.DefaultCandidateRefreshIntervalSecondsMax,
		HeldPositionIntervalSecondsMin:     config.DefaultHeldPositionIntervalSecondsMin,
		HeldPositionIntervalSecondsMax:     config.DefaultHeldPositionIntervalSecondsMax,
	}
	tests := []struct {
		name string
		yaml string
		want config.ScanConfig
	}{
		{"no scan section", "fast_screener:\n  top_n: 20\n", defaults},
		{
			"explicit zeros and negatives",
			"scan:\n  full_scan_interval_seconds: 0\n  candidate_refresh_interval_seconds_min: -1\n" +
				"  candidate_refresh_interval_seconds_max: 0\n  held_position_interval_seconds_min: 0\n",
			defaults,
		},
		{
			"valid values are kept",
			"scan:\n  full_scan_interval_seconds: 120\n" +
				"  candidate_refresh_interval_seconds_min: 20\n  candidate_refresh_interval_seconds_max: 40\n" +
				"  held_position_interval_seconds_min: 2\n  held_position_interval_seconds_max: 3\n",
			config.ScanConfig{
				FullScanIntervalSeconds:            120,
				CandidateRefreshIntervalSecondsMin: 20,
				CandidateRefreshIntervalSecondsMax: 40,
				HeldPositionIntervalSecondsMin:     2,
				HeldPositionIntervalSecondsMax:     3,
			},
		},
		{
			"max below min is raised to min",
			"scan:\n  full_scan_interval_seconds: 60\n" +
				"  candidate_refresh_interval_seconds_min: 50\n  candidate_refresh_interval_seconds_max: 40\n" +
				"  held_position_interval_seconds_min: 30\n",
			config.ScanConfig{
				FullScanIntervalSeconds:            60,
				CandidateRefreshIntervalSecondsMin: 50,
				CandidateRefreshIntervalSecondsMax: 50,
				HeldPositionIntervalSecondsMin:     30,
				HeldPositionIntervalSecondsMax:     30,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadStrategyBytes([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("LoadStrategyBytes returned error: %v", err)
			}
			got := cfg.Scan
			got.EventTrigger = config.EventTriggerConfig{}
			if got != tc.want {
				t.Errorf("Scan intervals = %+v, want %+v", got, tc.want)
			}
		})
	}
}

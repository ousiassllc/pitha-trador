package strategyflow_test

import (
	"reflect"
	"testing"
	"time"

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
		JevScoutMinIntervalSeconds:         config.DefaultJevScoutMinIntervalSeconds,
		KabuInfoAPIMaxPerSecond:            config.DefaultKabuInfoAPIMaxPerSecond,
		FullScanMaxSnapshotAgeSeconds:      620, // ceil(4000/8) + 2×60 (issue #686)
	}
	tests := []struct {
		name string
		yaml string
		want config.ScanConfig
	}{
		{"no scan section", "", defaults},
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
				"  held_position_interval_seconds_min: 2\n  held_position_interval_seconds_max: 3\n" +
				"  jev_scout_min_interval_seconds: 90\n",
			config.ScanConfig{
				FullScanIntervalSeconds:            120,
				CandidateRefreshIntervalSecondsMin: 20,
				CandidateRefreshIntervalSecondsMax: 40,
				HeldPositionIntervalSecondsMin:     2,
				HeldPositionIntervalSecondsMax:     3,
				JevScoutMinIntervalSeconds:         90,
				KabuInfoAPIMaxPerSecond:            config.DefaultKabuInfoAPIMaxPerSecond,
				FullScanMaxSnapshotAgeSeconds:      740, // 500 + 2×120
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
				JevScoutMinIntervalSeconds:         config.DefaultJevScoutMinIntervalSeconds,
				KabuInfoAPIMaxPerSecond:            config.DefaultKabuInfoAPIMaxPerSecond,
				FullScanMaxSnapshotAgeSeconds:      620,
			},
		},
		{
			"unset age follows a lowered rate cap",
			"scan:\n  kabu_info_api_max_per_second: 4\n",
			config.ScanConfig{
				FullScanIntervalSeconds:            config.DefaultFullScanIntervalSeconds,
				CandidateRefreshIntervalSecondsMin: config.DefaultCandidateRefreshIntervalSecondsMin,
				CandidateRefreshIntervalSecondsMax: config.DefaultCandidateRefreshIntervalSecondsMax,
				HeldPositionIntervalSecondsMin:     config.DefaultHeldPositionIntervalSecondsMin,
				HeldPositionIntervalSecondsMax:     config.DefaultHeldPositionIntervalSecondsMax,
				JevScoutMinIntervalSeconds:         config.DefaultJevScoutMinIntervalSeconds,
				KabuInfoAPIMaxPerSecond:            4,
				FullScanMaxSnapshotAgeSeconds:      1120, // 1000 + 2×60
			},
		},
		{
			"explicit full-scan snapshot age is kept",
			"scan:\n  full_scan_max_snapshot_age_seconds: 900\n",
			config.ScanConfig{
				FullScanIntervalSeconds:            config.DefaultFullScanIntervalSeconds,
				CandidateRefreshIntervalSecondsMin: config.DefaultCandidateRefreshIntervalSecondsMin,
				CandidateRefreshIntervalSecondsMax: config.DefaultCandidateRefreshIntervalSecondsMax,
				HeldPositionIntervalSecondsMin:     config.DefaultHeldPositionIntervalSecondsMin,
				HeldPositionIntervalSecondsMax:     config.DefaultHeldPositionIntervalSecondsMax,
				JevScoutMinIntervalSeconds:         config.DefaultJevScoutMinIntervalSeconds,
				KabuInfoAPIMaxPerSecond:            config.DefaultKabuInfoAPIMaxPerSecond,
				FullScanMaxSnapshotAgeSeconds:      900,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadStrategyBytes([]byte(tc.yaml + validStrategySections))
			if err != nil {
				t.Fatalf("LoadStrategyBytes returned error: %v", err)
			}
			got := cfg.Scan
			got.EventTrigger = config.EventTriggerConfig{}
			got.RankingMeasure = config.RankingMeasureConfig{}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Scan intervals = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Issue #686: ranking-watch mode keeps the caller's (3-minute) age; only
// full-scan mode switches to scan.full_scan_max_snapshot_age_seconds.
func TestScanConfig_SnapshotMaxAge(t *testing.T) {
	on, off := true, false
	const rankingAge = 3 * time.Minute
	tests := []struct {
		name string
		cfg  config.ScanConfig
		want time.Duration
	}{
		{"full scan unset", config.ScanConfig{FullScanMaxSnapshotAgeSeconds: 620}, rankingAge},
		{"full scan off", config.ScanConfig{FullScanEnabled: &off, FullScanMaxSnapshotAgeSeconds: 620}, rankingAge},
		{"full scan on", config.ScanConfig{FullScanEnabled: &on, FullScanMaxSnapshotAgeSeconds: 620}, 620 * time.Second},
		{"full scan on without an age", config.ScanConfig{FullScanEnabled: &on}, rankingAge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.SnapshotMaxAge(rankingAge); got != tc.want {
				t.Errorf("SnapshotMaxAge = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadStrategyBytes_KabuInfoAPIMaxPerSecond(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int
	}{
		{"explicit 5 is kept", "scan:\n  kabu_info_api_max_per_second: 5\n", 5},
		{"above official cap is clamped to 10", "scan:\n  kabu_info_api_max_per_second: 50\n", 10},
		{"zero is filled with default 8", "scan:\n  kabu_info_api_max_per_second: 0\n", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadStrategyBytes([]byte(tc.yaml + validStrategySections))
			if err != nil {
				t.Fatalf("LoadStrategyBytes: %v", err)
			}
			if got := cfg.Scan.KabuInfoAPIMaxPerSecond; got != tc.want {
				t.Errorf("KabuInfoAPIMaxPerSecond = %d, want %d", got, tc.want)
			}
		})
	}
}

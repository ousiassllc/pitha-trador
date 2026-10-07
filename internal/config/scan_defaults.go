package config

import "log/slog"

// Default scan.* intervals (seconds); each equals the shipped
// config/strategy.yaml value. They fill in keys an operator's older
// on-disk strategy.yaml lacks (see withScanIntervalDefaults).
const (
	DefaultFullScanIntervalSeconds            = 60
	DefaultCandidateRefreshIntervalSecondsMin = 15
	DefaultCandidateRefreshIntervalSecondsMax = 30
	DefaultHeldPositionIntervalSecondsMin     = 5
	DefaultHeldPositionIntervalSecondsMax     = 15
	DefaultJevScoutMinIntervalSeconds         = 60
	// DefaultKabuInfoAPIMaxPerSecond matches marketdata/infolimit.DefaultMaxPerSecond.
	DefaultKabuInfoAPIMaxPerSecond = 8
	// OfficialKabuInfoAPIMaxPerSecond is the kabuステーションAPI FAQ cap.
	OfficialKabuInfoAPIMaxPerSecond = 10
	// FullScanUniverseEstimate is the REST universe size (functional.md
	// FR-SCAN-3: 有効なstock全件、最大約4,000) the default full-scan snapshot
	// age is derived from.
	FullScanUniverseEstimate = 4000
	// fullScanSnapshotAgeTicks is how many full_scan_interval_seconds ticks the
	// derived full-scan snapshot age adds on top of one full REST cycle: one
	// for the wait before the next cycle starts (the next 60s tick after the
	// previous cycle ends) and one of margin.
	fullScanSnapshotAgeTicks = 2
)

// withScanIntervalDefaults makes every scan.*_seconds interval usable: an
// unset (or not positive) value is replaced by the shipped default, and a
// max below its min is raised to the min, each with a warning so the
// operator can fix strategy.yaml. A 0 interval - the Go zero value of a key
// missing from a strategy.yaml predating it - would otherwise spin the
// candidate refresh loop with no wait and register a "@every 0s" full scan.
func withScanIntervalDefaults(cfg *ScanConfig) {
	fill := func(key string, dest *int, def int) {
		if *dest > 0 {
			return
		}
		slog.Warn("config: scan."+key+" is not set (or not positive) in strategy.yaml; using default",
			"default_seconds", def)
		*dest = def
	}
	clampMax := func(key string, minV int, maxV *int) {
		if *maxV >= minV {
			return
		}
		slog.Warn("config: scan."+key+" is below its min in strategy.yaml; using the min",
			"min_seconds", minV, "max_seconds", *maxV)
		*maxV = minV
	}
	fill("full_scan_interval_seconds", &cfg.FullScanIntervalSeconds, DefaultFullScanIntervalSeconds)
	fill("candidate_refresh_interval_seconds_min", &cfg.CandidateRefreshIntervalSecondsMin, DefaultCandidateRefreshIntervalSecondsMin)
	fill("candidate_refresh_interval_seconds_max", &cfg.CandidateRefreshIntervalSecondsMax, DefaultCandidateRefreshIntervalSecondsMax)
	clampMax("candidate_refresh_interval_seconds_max", cfg.CandidateRefreshIntervalSecondsMin, &cfg.CandidateRefreshIntervalSecondsMax)
	fill("held_position_interval_seconds_min", &cfg.HeldPositionIntervalSecondsMin, DefaultHeldPositionIntervalSecondsMin)
	fill("held_position_interval_seconds_max", &cfg.HeldPositionIntervalSecondsMax, DefaultHeldPositionIntervalSecondsMax)
	clampMax("held_position_interval_seconds_max", cfg.HeldPositionIntervalSecondsMin, &cfg.HeldPositionIntervalSecondsMax)
	fill("jev_scout_min_interval_seconds", &cfg.JevScoutMinIntervalSeconds, DefaultJevScoutMinIntervalSeconds)
	if cfg.KabuInfoAPIMaxPerSecond <= 0 {
		slog.Warn("config: scan.kabu_info_api_max_per_second is not set (or not positive) in strategy.yaml; using default",
			"default", DefaultKabuInfoAPIMaxPerSecond)
		cfg.KabuInfoAPIMaxPerSecond = DefaultKabuInfoAPIMaxPerSecond
	}
	if cfg.KabuInfoAPIMaxPerSecond > OfficialKabuInfoAPIMaxPerSecond {
		slog.Warn("config: scan.kabu_info_api_max_per_second exceeds the official kabu info-API cap; clamping",
			"requested", cfg.KabuInfoAPIMaxPerSecond, "official_cap", OfficialKabuInfoAPIMaxPerSecond)
		cfg.KabuInfoAPIMaxPerSecond = OfficialKabuInfoAPIMaxPerSecond
	}
	if cfg.FullScanMaxSnapshotAgeSeconds <= 0 {
		cfg.FullScanMaxSnapshotAgeSeconds = DefaultFullScanMaxSnapshotAgeSeconds(cfg.KabuInfoAPIMaxPerSecond, cfg.FullScanIntervalSeconds)
		if cfg.FullScanOn() {
			slog.Warn("config: scan.full_scan_max_snapshot_age_seconds is not set (or not positive) in strategy.yaml; deriving it from the info-API rate cap",
				"default_seconds", cfg.FullScanMaxSnapshotAgeSeconds)
		}
	}
}

// DefaultFullScanMaxSnapshotAgeSeconds derives the full-scan snapshot age
// that covers one full REST cycle (FullScanUniverseEstimate symbols at
// maxPerSecond calls/s, rounded up) plus the wait for the next cycle and a
// margin (fullScanSnapshotAgeTicks × intervalSeconds): 500 + 120 = 620 s with
// the shipped 8/s and 60 s (issue #686).
func DefaultFullScanMaxSnapshotAgeSeconds(maxPerSecond, intervalSeconds int) int {
	cycle := (FullScanUniverseEstimate + maxPerSecond - 1) / maxPerSecond
	return cycle + fullScanSnapshotAgeTicks*intervalSeconds
}

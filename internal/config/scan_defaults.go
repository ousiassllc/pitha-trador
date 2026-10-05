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
}

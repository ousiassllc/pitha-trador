// Package assist implements the Sol (Think) and Opus (Govern) adapters
// of the self-improvement loop (docs/requirements/functional.md §4.14,
// docs/architecture/overview.md §8): Sol turns Calibration's Brier
// Score/ECE/confidence-bucket statistics into a candidate Policy Engine
// threshold change (FR-SELFIMPROVE-1), and Opus turns a shadow
// backtest's Expectancy/MaxDrawdown comparison into an approve/reject
// decision against FR-SELFIMPROVE-4's exact numeric rule. Both adapters
// are pure, deterministic Go functions with no I/O of their own -
// internal/service/selfimprove.Governor supplies every input (gathered
// from internal/repository) and persists every output
// (policy_proposals); a future real Sol/Opus external API integration
// (architecture/overview.md §2 "Luna/Sol/Opusアダプタ | 独自HTTPクライア
// ント") only needs to replace this package's Analyze/Review bodies, not
// their callers.
//
// Neither Sol nor Opus can write risk.* runtime_settings keys or Jev's
// prompt_version: this package exposes no API that accepts an arbitrary
// runtime_settings key/value pair at all, only a
// domain.PolicyChange whose Key internal/domain.ValidatePolicyChanges
// restricts to the policy.* threshold keys FR-SELFIMPROVE-2 allows
// (overview.md §1 "AI自己改善ループの境界").
package assist

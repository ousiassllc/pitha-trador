// Package assist implements the external-AI adapters of the
// docs/architecture/overview.md §2 "Luna/Sol/Opusアダプタ | 独自HTTPクライ
// アント": Luna (Sense, §13: news classification, FR-LUNA-2), Sol (Think,
// §8: daily threshold-change proposals, FR-SELFIMPROVE-1) and Opus
// (Govern, §8: qualitative proposal review, FR-SELFIMPROVE-9). Each is a
// thin wrapper over Client, a JSON-over-HTTP client with Bearer
// authentication and Jev's retry/exponential-backoff policy, calling an
// actual external AI API configured from the Settings screen
// (LUNA_*/SOL_*/OPUS_* secrets). An unconfigured Client fails every call
// with ErrNotConfigured, which callers treat like any other API failure.
//
// LLM output is untrusted. Sol returns the changes it proposes verbatim
// (SolChange) and internal/service/selfimprove.Governor machine-validates
// them (FR-SELFIMPROVE-8). Opus first evaluates FR-SELFIMPROVE-4's
// deterministic Expectancy/Max Drawdown thresholds itself and only asks the
// Opus API when they pass, so the API can veto a threshold-passing
// proposal but never approve one that missed them (FR-SELFIMPROVE-9).
// Luna's classification is validated against its closed value sets and is
// only ever auxiliary context for Jev (FR-LUNA-5).
//
// Neither Sol nor Opus can write risk.* runtime_settings keys or Jev's
// prompt_version: this package exposes no API that accepts an arbitrary
// runtime_settings key/value pair at all, only a
// domain.PolicyChange whose Key internal/domain.ValidatePolicyChanges
// restricts to the policy.* threshold keys FR-SELFIMPROVE-2 allows
// (overview.md §1 "AI自己改善ループの境界").
package assist

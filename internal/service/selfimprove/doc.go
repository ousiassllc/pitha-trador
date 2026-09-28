// Package selfimprove implements the Self-Improvement Governor
// (docs/requirements/functional.md §4.14, docs/architecture/overview.md
// §8, FR-SELFIMPROVE-1〜7): the daily Sol proposal -> shadow backtest ->
// Opus review -> runtime_settings apply -> post-apply rollback pipeline
// tying internal/service/assist's Sol/Opus adapters,
// internal/service/backtest's reusable engine, and
// internal/repository.ProposalRepository/RuntimeSettingsRepository/
// PositionRepository together.
//
// Governor is the only place in this codebase that writes
// runtime_settings' policy.* keys on Sol/Opus's behalf, and it never
// accepts an arbitrary runtime_settings key/value pair to do so - only a
// domain.PolicyChange, whose Key domain.ValidatePolicyChanges restricts
// to the policy.* keys FR-SELFIMPROVE-2 allows. There is no Governor
// method, and no assist.Sol/assist.Opus method, that writes a risk.*
// key or Jev's prompt_version: that write API simply does not exist in
// either package (overview.md §1 "AI自己改善ループの境界",
// docs/architecture/overview.md §8 "risk.*キーとJevのprompt_versionは
// selfimproveサービスに書き込みAPIそのものを持たせないことで技術的に
// 強制する").
package selfimprove

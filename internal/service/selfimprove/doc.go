// Package selfimprove implements the Self-Improvement Governor
// (docs/requirements/functional.md §4.14, docs/architecture/overview.md
// §8, FR-SELFIMPROVE-1〜7): the daily Sol proposal -> shadow backtest ->
// Opus review -> runtime_settings apply -> post-apply rollback pipeline
// tying internal/service/assist's Sol/Opus adapters,
// internal/service/backtest's reusable engine, and
// internal/repository/judgement.ProposalRepository/RuntimeSettingsRepository/
// PositionRepository together.
//
// Sol and Opus are real external LLM API calls (FR-SELFIMPROVE-8/9). The
// Governor treats their output as untrusted: Sol's proposed changes are
// validated mechanically (policy.* keys only, change-width caps) and a
// violation is recorded as status=rejected with
// review_json.reason=llm_output_out_of_bounds without any backtest or
// Opus call; Opus can veto a proposal that met the deterministic
// FR-SELFIMPROVE-4 thresholds but can never approve one that missed them.
// An API failure skips that stage for the day (Notifier.AIStageSkipped)
// and RunDaily retries it the next business day.
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

package jev

// Prompt versions (issue #872).
//
// The prompt version of a Jev decision is jev_decisions.question_version: NOT
// NULL since migration 000004 and filled by every Scout and Trader row. It is
// the formal prompt version; no prompt_version column was added (one fact in
// two columns could disagree; see docs/architecture/er/tables-market.md
// §jev_decisions). DecisionRepository.Insert refuses an empty version. The
// shadow's table jev_shadow_decisions names its candidate version prompt_version.
//
// A version is bound to its wording by hash: promptflow/prompt_version_test.go pins
// the SHA-256 of every registered question group (prompts.go), so changing a
// wording without a new version fails the test. A changed wording is a new version.
//
// What a version means for the data (issue #881): jev_decisions rows of Trader
// versions of different cohorts (cohort.go) are never aggregated or learned
// together. The RAG candidates, the Calibration samples and the meta-model
// training keep the live version's cohort only; jev_calibration grades that
// cohort by default and any one version on request
// (docs/architecture/jev-prompt-versions-in-data.md).
//
// Which version is LIVE is not these constants alone: they are the build
// defaults, and a promotion (GUI or MCP jev_prompt_promote only, never automatic,
// never the self-improvement loop) stores another registered version in
// runtime_settings (jevshadowsettings.KeyLiveVersion), which Trader reads on every
// call (LiveVersion). The shadow runs a candidate beside it (jev/shadow).

// ScoutQuestionVersion is the Jev Scout question-set/prompt version this
// build sends and records to jev_decisions.question_version
// (architecture/overview.md §6, architecture/er.md §jev_decisions).
//
// Bump this constant whenever the Scout question wording or prompt
// structure changes materially, so decisions produced "before" and
// "after" the change stay distinguishable in jev_decisions. The Scout has no
// promotion and its decisions are not graded, so the version is recorded and
// shown (Decision history/Activity) only; for the Trader a version is also what
// the aggregation and the training keep apart (cohort.go, issue #881).
//
// History: scout-v3 rewrites the shared stateGuide so it describes the
// realized outcomes (`future_return`, `was_direction_correct`,
// `horizon_minutes`, `regime`) the RAG context now carries (FR-RAG-3);
// scout-v2 told Jev that similar cases were never realized outcomes.
const ScoutQuestionVersion = "scout-v3"

// TraderQuestionVersion is the Jev Trader question-set/prompt version this
// build sends and records to jev_decisions.question_version while no
// promotion has stored another one (LiveVersion), using the same versioning
// rationale as ScoutQuestionVersion. Never edit the wording of a registered
// version (prompts.go): add a new one.
//
// History: trader-v3 is the same stateGuide rewrite as scout-v3. trader-v4
// (TraderEntryQualityCandidateVersion) is a registered CANDIDATE with a
// redefined entry_quality; trader-v5 (TraderExitPlanCandidateVersion) is a
// registered CANDIDATE whose direction and continuation_probability questions
// name the Paper exit rules (issue #925). Neither is the live version and
// nothing promotes them automatically.
const TraderQuestionVersion = "trader-v3"

package jev

// ScoutQuestionVersion is the Jev Scout question-set/prompt version this
// build sends and records to jev_decisions.question_version
// (architecture/overview.md §6, architecture/er.md §jev_decisions).
//
// Bump this constant whenever the Scout question wording or prompt
// structure changes materially, so decisions produced "before" and
// "after" the change stay distinguishable in jev_decisions. The version is
// recorded and shown (Decision history/Activity) only: Calibration
// aggregation (ListLabeledSamples*, selfimprove daily analysis) does not
// split by question_version, so right after a bump old- and new-version
// decisions are mixed in the same metrics.
//
// History: scout-v3 rewrites the shared stateGuide so it describes the
// realized outcomes (`future_return`, `was_direction_correct`,
// `horizon_minutes`, `regime`) the RAG context now carries (FR-RAG-3);
// scout-v2 told Jev that similar cases were never realized outcomes.
const ScoutQuestionVersion = "scout-v3"

// TraderQuestionVersion is the Jev Trader question-set/prompt version
// this build sends and records to jev_decisions.question_version, using
// the same versioning rationale (recording only, no Calibration cohort
// split) as ScoutQuestionVersion. Bump it whenever the Trader question
// wording or prompt structure changes materially.
//
// History: trader-v3 is the same stateGuide rewrite as scout-v3.
const TraderQuestionVersion = "trader-v3"

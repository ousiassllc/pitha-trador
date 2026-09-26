package jev

// ScoutQuestionVersion is the Jev Scout question-set/prompt version this
// build sends and records to jev_decisions.question_version
// (architecture/overview.md §6, architecture/er.md §jev_decisions).
//
// Bump this constant whenever the Scout question wording or prompt
// structure changes materially enough that Calibration should distinguish
// decisions produced "before" from "after" the change (functional.md
// §4.12 groups/analyzes jev_decisions by question_version so a wording
// change doesn't silently contaminate an existing calibration cohort).
const ScoutQuestionVersion = "scout-v1"

// TraderQuestionVersion is the Jev Trader question-set/prompt version
// this build sends and records to jev_decisions.question_version, using
// the same versioning rationale as ScoutQuestionVersion (functional.md
// §4.12 groups/analyzes jev_decisions by question_version so a wording
// change doesn't silently contaminate an existing calibration cohort).
// Bump it whenever the Trader question wording or prompt structure
// changes materially enough that Calibration should distinguish
// decisions produced "before" from "after" the change.
const TraderQuestionVersion = "trader-v1"

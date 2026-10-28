package jev

import (
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/exitplan"
)

// TraderExitPlanCandidateVersion is the second candidate prompt (issue #925):
// trader-v3 with the direction and continuation_probability questions asked
// against the Paper exits (exitplan has the wording and the reason). It is a
// CANDIDATE: the live default stays TraderQuestionVersion and only an explicit
// promotion changes it. trader-v4 is taken (entry_quality, #872 / #881 / #883).
const TraderExitPlanCandidateVersion = "trader-v5"

var traderQuestionsV5 = exitplan.Questions(traderQuestions, execution.DefaultConfig())

package jev

import (
	"maps"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// TraderEntryQualityCandidateVersion is the first candidate prompt (issue
// #872): trader-v3 with a redefined entry_quality (the Instructions of
// traderQuestions[qEntryQuality] and the Rubrics of poor, fair and good).
// Every other question is the trader-v3 one, so a difference between the two
// versions' answers can only come from the entry_quality definition.
//
// It is a CANDIDATE, not the live version: the live default stays
// TraderQuestionVersion, nothing promotes it automatically, and the shadow
// that runs it is off until a person turns it on (jevshadowsettings). Whether
// it is better is decided by outcomes, never by how its distribution looks.
//
// Why: over 30 days trader-v3 rated entry_quality poor 903, fair 358, good 25,
// strong 4, exceptional 0 (issue #861), so nearly every candidate fell below
// any min_entry_quality that could lead to a trade. Two wordings of trader-v3
// push towards poor: the poor Rubric is a list of alternatives that any one of
// which triggers it ("or no momentum to follow", "a wide spread"), and the
// good Rubric asks for confirmation on every axis at once.
const TraderEntryQualityCandidateVersion = "trader-v4"

// entryQualityOptionsV4 is entryQualityOptions with poor, fair and good
// redefined (strong and exceptional are the trader-v3 texts, unchanged).
var entryQualityOptionsV4 = []systemone.Option{
	{Name: domain.JevEntryQualityPoor, Rubric: "A clear flaw makes the entry unattractive: chasing an extended or exhausted move, entering against order flow or VWAP, a spread that eats the likely gain, or no momentum to follow. An unremarkable setup is not poor."},
	{Name: domain.JevEntryQualityFair, Rubric: "Some support but no clear edge: the setup is incomplete (for example volume confirmation is weak or price is stretched from VWAP) and the reward is not clearly above the risk and the spread cost, yet there is no clear flaw as in poor."},
	{Name: domain.JevEntryQualityGood, Rubric: "A sound, ordinary entry: price on the favourable side of VWAP, volume at or above normal, order flow not against it, a moderate stretch from VWAP and an acceptable spread. A clean continuation setup without a standout feature is good."},
	entryQualityOptions[3],
	entryQualityOptions[4],
}

// entryQualityInstructionsV4 is the trader-v4 Instructions of the
// entry_quality question: the trader-v3 factors, plus how to choose among the
// levels so that good is the rating of an ordinary sound setup.
const entryQualityInstructionsV4 = "Rate the quality of entering a position with the current momentum of `market` right now: the entry point itself (timing, stretch and risk/reward), not just how strong the momentum is. " +
	"Rate the entry in the direction the evidence in `market` favours; if no direction is favoured, answer poor. " +
	"Go through the levels from poor upwards and answer the highest level whose description is met. A level describes a typical case, not a requirement that every factor is perfect: " +
	"a clean, ordinary continuation setup is good, and strong and exceptional are the better-than-ordinary cases. Use poor only for a clear flaw, not for a setup that is merely unremarkable. " +
	"Consider how far price is extended from VWAP (`price_vs_vwap_bps`) and from the 5-minute extremes, whether the move is just starting or already stretched (`return_5m` against `return_15m` and `return_30m`), " +
	"the spread cost (`spread_bps`), volume confirmation (`volume_ratio_5m`) and order-flow support."

// traderQuestionsV4 is the trader-v4 question group: traderQuestions with only
// the entry_quality question replaced.
var traderQuestionsV4 = func() map[string]systemone.Question {
	questions := maps.Clone(traderQuestions)
	questions[qEntryQuality] = systemone.ChoiceQuestion(withStateGuide(entryQualityInstructionsV4), entryQualityOptionsV4...)
	return questions
}()

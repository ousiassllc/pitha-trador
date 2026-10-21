package jev

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// traderPrompts is every Trader prompt version this build can send, by version
// name: the live version and the candidates the shadow may run side by side
// (issue #872). A version is the exact question set it names: the Instructions
// and the option Rubrics. evalflow/prompt_version_test.go pins a hash of each, so a
// wording change without a new version fails the build.
//
// Adding a version: add its question group, register it here, add its hash to
// the test's pin table. Never edit a registered group: that silently changes
// what rows recorded under its name mean (jev_decisions.question_version).
var traderPrompts = map[string]map[string]systemone.Question{
	TraderQuestionVersion:              traderQuestions,
	TraderEntryQualityCandidateVersion: traderQuestionsV4,
}

// TraderVersions returns every registered Trader prompt version, sorted.
func TraderVersions() []string {
	versions := make([]string, 0, len(traderPrompts))
	for v := range traderPrompts {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions
}

// IsTraderVersion reports whether version is a registered Trader prompt version.
func IsTraderVersion(version string) bool {
	_, ok := traderPrompts[version]
	return ok
}

// TraderQuestionsFor returns the Trader question group of version, so the live
// version and a candidate can be built in the same process. An empty version
// is the build default (TraderQuestionVersion); an unregistered one is an
// error (a version nobody defined must never be sent as if it were another).
func TraderQuestionsFor(version string) (map[string]systemone.Question, error) {
	if version == "" {
		version = TraderQuestionVersion
	}
	questions, ok := traderPrompts[version]
	if !ok {
		return nil, fmt.Errorf("jev: unknown trader prompt version %q", version)
	}
	return questions, nil
}

// TraderPromptHash returns the SHA-256 (hex) of the wire form of version's
// question group (the JSON the API receives: every Instructions text and every
// option Rubric, the option order included).
func TraderPromptHash(version string) (string, error) {
	questions, err := TraderQuestionsFor(version)
	if err != nil {
		return "", err
	}
	return hashQuestions(questions)
}

// ScoutPromptHash returns the same hash for ScoutQuestionVersion's question group.
func ScoutPromptHash() (string, error) { return hashQuestions(scoutQuestions) }

// hashQuestions hashes the canonical JSON of questions: encoding/json sorts the
// map keys and systemone.Options keeps the declared option order, so the bytes
// depend only on the wording.
func hashQuestions(questions map[string]systemone.Question) (string, error) {
	body, err := json.Marshal(questions)
	if err != nil {
		return "", fmt.Errorf("jev: encode questions for hashing: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

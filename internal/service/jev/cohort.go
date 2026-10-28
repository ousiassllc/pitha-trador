package jev

import (
	"context"
	"fmt"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Trader prompt cohorts (issue #881).
//
// A prompt version names an exact wording (prompts.go). A cohort groups the
// versions whose answers MEAN the same thing, so their jev_decisions rows may be
// aggregated and learned together. A promotion switches the live version; the
// consumers that read jev_decisions as evidence about the live prompt (the RAG
// few-shot candidates, the Calibration samples, the meta-model training) keep
// only the live version's cohort and leave the rows of every other cohort out,
// so a promotion starts a fresh sample instead of blending two prompts'
// answers. The meta-model reads the entry_quality answer as a feature
// (TraderResponse.Score), so a redefined entry_quality is a different cohort
// even though the other questions did not change.
//
// Adding a version means choosing its cohort (traderCohorts); a test fails
// until it is registered. Put a version in an existing cohort only when the
// answers it records mean exactly what that cohort's answers mean (a rewording
// of the shared stateGuide that leaves every question's definition alone, as
// trader-v2 to trader-v3, is one). Anything that redefines what an answer means
// (an Instructions or a Rubric of a question the model or Policy reads) starts a
// new cohort.
const (
	// cohortEntryQualityV3 is the entry_quality of trader-v3: the Rubrics of
	// questions_trader.go.
	cohortEntryQualityV3 = "entry-quality-v3"
	// cohortEntryQualityV4 is the redefined entry_quality of trader-v4
	// (questions_trader_v4.go).
	cohortEntryQualityV4 = "entry-quality-v4"
	// cohortExitPlanV5 is the direction and continuation_probability of
	// trader-v5 (questions_trader_v5.go): asked against the Paper exit rules
	// instead of "the next several minutes", so Policy reads another answer
	// than trader-v3's even though entry_quality is the trader-v3 one.
	cohortExitPlanV5 = "exit-plan-v5"
)

// traderCohorts is the cohort of every Trader prompt version that has ever
// written a jev_decisions row: every registered version (traderPrompts) and the
// retired ones, which still sit in the table. trader-v1 is the pre-#263 schema
// and trader-v2 the stateGuide before the realized outcomes were put into the
// RAG context (prompt_version.go): both answer entry_quality as trader-v3 does,
// so they stay with it, which keeps a promotion from silently changing what the
// rows of today's database select.
var traderCohorts = map[string]string{
	"trader-v1":                        cohortEntryQualityV3,
	"trader-v2":                        cohortEntryQualityV3,
	TraderQuestionVersion:              cohortEntryQualityV3,
	TraderEntryQualityCandidateVersion: cohortEntryQualityV4,
	TraderExitPlanCandidateVersion:     cohortExitPlanV5,
}

// TraderCohort returns the cohort of version and whether the version is known.
func TraderCohort(version string) (string, bool) {
	c, ok := traderCohorts[version]
	return c, ok
}

// ForeignVersions returns, sorted, every known Trader version whose cohort is
// not live's: the versions whose rows stay out of the live version's
// aggregation and training. A live version that is not known has no foreign
// versions (nothing is left out).
func ForeignVersions(live string) []string {
	cohort, ok := traderCohorts[live]
	if !ok {
		return []string{}
	}
	out := []string{}
	for v, c := range traderCohorts {
		if c != cohort {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// ForeignVersionsOfLive is ForeignVersions of the live version store resolves
// (ResolveLiveVersion): a failed read is an error, never "no foreign versions",
// so that a database that cannot say which version is live does not turn into a
// training set that mixes them.
func ForeignVersionsOfLive(ctx context.Context, store jevshadowsettings.Store) ([]string, error) {
	live, err := ResolveLiveVersion(ctx, store)
	if err != nil {
		return nil, err
	}
	return ForeignVersions(live), nil
}

// LiveCohortScope is ForeignVersionsOfLive as the sqlutil.ForeignTraderVersions
// the repositories and the RAG service take.
func LiveCohortScope(store jevshadowsettings.Store) sqlutil.ForeignTraderVersions {
	return func(ctx context.Context) ([]string, error) { return ForeignVersionsOfLive(ctx, store) }
}

// LiveCohort is the live Trader version (ResolveLiveVersion) and its cohort. A
// failed read of the live version is an error, never a default cohort. Every
// version ResolveLiveVersion returns is registered and so has a cohort
// (TestEveryRegisteredTraderVersionHasACohort); a version without one is an
// error all the same, so a cohort is never guessed.
func LiveCohort(ctx context.Context, store jevshadowsettings.Store) (version, cohort string, err error) {
	version, err = ResolveLiveVersion(ctx, store)
	if err != nil {
		return "", "", err
	}
	cohort, ok := traderCohorts[version]
	if !ok {
		return "", "", fmt.Errorf("jev: live trader version %q has no cohort", version)
	}
	return version, cohort, nil
}

package metaflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/decisionnote"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/metacohort"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

const (
	cohortV3 = "entry-quality-v3" // trader-v1..v3
	cohortV4 = "entry-quality-v4" // trader-v4
)

// liveCohort is the metacohort.Source of a live version (and its cohort).
func liveCohort(version, cohort string) metacohort.Source {
	return func(context.Context) (string, string, error) { return version, cohort, nil }
}

// modelTrainedOn is the taking model with its recorded training cohort ("" is
// a model stored before the cohort was recorded).
func modelTrainedOn(cohort string) metalabel.Model {
	m := policytest.TakingModel()
	m.TrainedCohort = cohort
	return m
}

// Issue #881: the model's training cohort against the live version's. Paper
// warns and keeps scoring with it; Live does not use it (not_calibrated). A
// model that recorded no cohort is the pre-promotion one (trader-v1..v3's).
func TestEvaluate_MetaModelCohortAgainstTheLiveVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		trained      string
		liveVersion  string
		liveCohort   string
		mode         metalabelsettings.Mode
		wantTake     bool
		wantNote     bool // meta_model_cohort_mismatch recorded on a taken decision
		wantNotCalib bool
	}{
		"Paper, trained on v3, live v4: warns and uses the model": {
			trained: cohortV3, liveVersion: "trader-v4", liveCohort: cohortV4, mode: metalabelsettings.ModePaper, wantTake: true, wantNote: true,
		},
		"Live, trained on v3, live v4: treated as not calibrated": {
			trained: cohortV3, liveVersion: "trader-v4", liveCohort: cohortV4, mode: metalabelsettings.ModeLive, wantNotCalib: true,
		},
		"legacy model (no cohort), live v3 (Paper): matches": {
			trained: "", liveVersion: "trader-v3", liveCohort: cohortV3, mode: metalabelsettings.ModePaper, wantTake: true,
		},
		"legacy model (no cohort), live v3 (Live): matches, unchanged": {
			trained: "", liveVersion: "trader-v3", liveCohort: cohortV3, mode: metalabelsettings.ModeLive, wantTake: true,
		},
		"legacy model (no cohort), live v4 (Paper): mismatch recorded": {
			trained: "", liveVersion: "trader-v4", liveCohort: cohortV4, mode: metalabelsettings.ModePaper, wantTake: true, wantNote: true,
		},
		"legacy model (no cohort), live v4 (Live): not calibrated": {
			trained: "", liveVersion: "trader-v4", liveCohort: cohortV4, mode: metalabelsettings.ModeLive, wantNotCalib: true,
		},
		"trained on v4, live v4: matches": {
			trained: cohortV4, liveVersion: "trader-v4", liveCohort: cohortV4, mode: metalabelsettings.ModeLive, wantTake: true,
		},
		"trained on v4, live v3 (rolled back), Live: not calibrated": {
			trained: cohortV4, liveVersion: "trader-v3", liveCohort: cohortV3, mode: metalabelsettings.ModeLive, wantNotCalib: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			logs := captureLogs(t)
			risk := &countingRisk{fakeRiskChecker: fakeRiskChecker{passed: true}}
			preds := &policytest.Predictions{}
			engine := policy.NewEngine(testThresholds(), risk, env.signals,
				policytest.Option(policytest.ModelOf(modelTrainedOn(tc.trained)), preds),
				policy.WithMetaModelCohort(tc.mode, liveCohort(tc.liveVersion, tc.liveCohort)))

			sig, err := engine.Evaluate(context.Background(), env.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			reason := reasonOf(sig)
			switch {
			case tc.wantTake:
				if sig.Direction != domain.JevDirectionLong || !sig.RiskPassed || risk.checks != 1 {
					t.Fatalf("signal = %+v (%s), risk checks %d, want a LONG entry that reached the Risk Engine", sig, reason, risk.checks)
				}
				if len(preds.Recorded()) != 1 {
					t.Errorf("recorded %d predictions, want 1 (the model scored)", len(preds.Recorded()))
				}
			case tc.wantNotCalib:
				want := policy.ReasonNotCalibrated + ": meta model lr-test was trained on cohort"
				if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || !strings.HasPrefix(reason, want) || !strings.Contains(reason, tc.liveVersion) {
					t.Fatalf("signal = %+v (%s), want NONE %q naming live version %s", sig, reason, want, tc.liveVersion)
				}
				if risk.checks != 0 || len(preds.Recorded()) != 0 {
					t.Errorf("Risk checks %d / predictions %d, want 0 / 0: the mismatching model must not score", risk.checks, len(preds.Recorded()))
				}
			}

			hasNote := strings.Contains(reason, decisionnote.MetaModelCohortMismatch)
			if hasNote != tc.wantNote {
				t.Errorf("reject_reason = %q, meta_model_cohort_mismatch recorded = %v, want %v", reason, hasNote, tc.wantNote)
			}
			warned := strings.Contains(logs.String(), "judged on a fallback or a mismatching meta model")
			if warned != tc.wantNote {
				t.Errorf("warning logged = %v, want %v; logs: %s", warned, tc.wantNote, logs.String())
			}
		})
	}
}

// A model that matches (or no cohort check wired) leaves the decision record as
// before: no note, a take has no reject_reason.
func TestEvaluate_MatchingOrUncheckedModelLeavesTheRecordUnchanged(t *testing.T) {
	for name, opts := range map[string][]policy.Option{
		"matching": {policy.WithMetaModelCohort(metalabelsettings.ModeLive, liveCohort("trader-v3", cohortV3))},
		"unwired":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			opts = append(opts, policytest.Option(policytest.ModelOf(modelTrainedOn(cohortV3)), &policytest.Predictions{}))
			sig, err := env.newEngine(opts...).Evaluate(context.Background(), env.in)
			if err != nil || sig.Direction != domain.JevDirectionLong || sig.RejectReason != nil {
				t.Fatalf("signal = %+v, %v, want a LONG with no reject_reason", sig, err)
			}
		})
	}
}

// With no model there is nothing to compare: the no_active_model skip is
// unchanged and the live cohort is not even read.
func TestEvaluate_NoModelDoesNotReadTheLiveCohort(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	reads := 0
	src := func(context.Context) (string, string, error) { reads++; return "trader-v3", cohortV3, nil }
	sig, err := env.newEngine(policytest.Option(policytest.Models{}, &policytest.Predictions{}),
		policy.WithMetaModelCohort(metalabelsettings.ModeLive, src)).Evaluate(context.Background(), env.in)
	if err != nil || !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaSkip+": "+metalabel.ReasonNoModel) || reads != 0 {
		t.Fatalf("signal = %+v (%s), cohort reads %d, %v", sig, reasonOf(sig), reads, err)
	}
}

// A live cohort that cannot be read: Live skips the entry (never guessed), Paper
// keeps the model and logs the failure.
func TestEvaluate_UnreadableLiveCohort(t *testing.T) {
	failing := func(context.Context) (string, string, error) { return "", "", errors.New("settings unreadable") }

	env := newMetaEnv(t, domain.JevDirectionLong)
	sig, err := env.newEngine(policytest.Option(policytest.ModelOf(modelTrainedOn(cohortV3)), &policytest.Predictions{}),
		policy.WithMetaModelCohort(metalabelsettings.ModeLive, failing)).Evaluate(context.Background(), env.in)
	if err != nil || sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaModelUnavailable) {
		t.Fatalf("Live: signal = %+v (%s), %v, want NONE %s", sig, reasonOf(sig), err, policy.ReasonMetaModelUnavailable)
	}

	env = newMetaEnv(t, domain.JevDirectionLong)
	logs := captureLogs(t)
	sig, err = env.newEngine(policytest.Option(policytest.ModelOf(modelTrainedOn(cohortV3)), &policytest.Predictions{}),
		policy.WithMetaModelCohort(metalabelsettings.ModePaper, failing)).Evaluate(context.Background(), env.in)
	if err != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("Paper: signal = %+v (%s), %v, want the model's LONG", sig, reasonOf(sig), err)
	}
	if !strings.Contains(logs.String(), "read the live prompt cohort") {
		t.Errorf("the unreadable live cohort was not logged: %s", logs.String())
	}
}

package metacohort_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/metacohort"
)

const (
	cohortV3 = jev.LegacyModelCohort // trader-v1..v3
	cohortV4 = "entry-quality-v4"    // trader-v4
)

func source(version, cohort string) metacohort.Source {
	return func(context.Context) (string, string, error) { return version, cohort, nil }
}

func failing(context.Context) (string, string, error) {
	return "", "", errors.New("settings unreadable")
}

// Check by mode: only Paper lets a model that does not fit the live cohort (or
// whose live cohort cannot be read) keep scoring; every other mode, including
// none, does not use it.
func TestChecker_Check(t *testing.T) {
	for name, tc := range map[string]struct {
		mode        metalabelsettings.Mode
		live        metacohort.Source
		trained     string
		wantNothing bool   // the verdict is empty: the model is used, nothing to record
		wantField   string // Mismatch, Uncalibrated or Unavailable
	}{
		"Paper, match":                        {mode: metalabelsettings.ModePaper, live: source("trader-v4", cohortV4), trained: cohortV4, wantNothing: true},
		"Live, match":                         {mode: metalabelsettings.ModeLive, live: source("trader-v4", cohortV4), trained: cohortV4, wantNothing: true},
		"Paper, mismatch is recorded":         {mode: metalabelsettings.ModePaper, live: source("trader-v4", cohortV4), trained: cohortV3, wantField: "Mismatch"},
		"Live, mismatch is not calibrated":    {mode: metalabelsettings.ModeLive, live: source("trader-v4", cohortV4), trained: cohortV3, wantField: "Uncalibrated"},
		"mode unset, mismatch is like Live":   {mode: "", live: source("trader-v4", cohortV4), trained: cohortV3, wantField: "Uncalibrated"},
		"mode unset, match":                   {mode: "", live: source("trader-v3", cohortV3), trained: cohortV3, wantNothing: true},
		"legacy model, live v3, Paper":        {mode: metalabelsettings.ModePaper, live: source("trader-v3", cohortV3), trained: "", wantNothing: true},
		"legacy model, live v3, Live":         {mode: metalabelsettings.ModeLive, live: source("trader-v3", cohortV3), trained: "", wantNothing: true},
		"legacy model, live v4, Paper":        {mode: metalabelsettings.ModePaper, live: source("trader-v4", cohortV4), trained: "", wantField: "Mismatch"},
		"legacy model, live v4, Live":         {mode: metalabelsettings.ModeLive, live: source("trader-v4", cohortV4), trained: "", wantField: "Uncalibrated"},
		"Paper, live cohort unreadable":       {mode: metalabelsettings.ModePaper, live: failing, trained: cohortV3, wantNothing: true},
		"Live, live cohort unreadable":        {mode: metalabelsettings.ModeLive, live: failing, trained: cohortV3, wantField: "Unavailable"},
		"mode unset, live cohort unreadable":  {mode: "", live: failing, trained: cohortV3, wantField: "Unavailable"},
		"Live, no live cohort source":         {mode: metalabelsettings.ModeLive, live: nil, trained: cohortV3, wantField: "Unavailable"},
		"Paper, no live cohort source":        {mode: metalabelsettings.ModePaper, live: nil, trained: cohortV3, wantNothing: true},
		"zero checker (nothing wired) fails":  {mode: "", live: nil, trained: cohortV3, wantField: "Unavailable"},
		"Live, trained on v4, live rolled v3": {mode: metalabelsettings.ModeLive, live: source("trader-v3", cohortV3), trained: cohortV4, wantField: "Uncalibrated"},
	} {
		t.Run(name, func(t *testing.T) {
			got := metacohort.Checker{Mode: tc.mode, Live: tc.live}.Check(context.Background(), "lr-1", tc.trained)
			fields := map[string]string{"Mismatch": got.Mismatch, "Uncalibrated": got.Uncalibrated, "Unavailable": got.Unavailable}
			for field, text := range fields {
				if set := text != ""; set != (field == tc.wantField) {
					t.Errorf("Check = %+v, %s set = %v, want only %q (or nothing: %v)", got, field, set, tc.wantField, tc.wantNothing)
				}
			}
			if tc.wantField == "Mismatch" || tc.wantField == "Uncalibrated" {
				if text := fields[tc.wantField]; !strings.Contains(text, "lr-1") || !strings.Contains(text, "live version trader-v") {
					t.Errorf("%s = %q, want it to name the model and the live version", tc.wantField, text)
				}
			}
		})
	}
}

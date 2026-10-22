package decisionnote_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/policy/decisionnote"
)

func TestAppend(t *testing.T) {
	reason := "meta_skip: ev_not_above_minimum"
	note := decisionnote.Note(decisionnote.CalibrationAllVersions, "judged on all-versions total; live version trader-v4 has fewer than 20 labeled samples in the confidence bucket")
	bare, risk, riskDetail, riskBare := "special_quote", "risk_engine_rejected: system_paused", "risk_engine_rejected: max_open_positions: count=3 max=3", "risk_engine_rejected"
	for name, tc := range map[string]struct {
		reason *string
		codes  int
		notes  []string
		want   string
	}{
		"no notes leaves the reason":                         {&reason, 1, nil, reason},
		"detail then the note":                               {&reason, 1, []string{note}, reason + "; " + note},
		"a passed signal gets the note alone":                {nil, 1, []string{note}, note},
		"an empty reason gets the note alone":                {new(string), 1, []string{note}, note},
		"two notes":                                          {&reason, 1, []string{"a: 1", "b: 2"}, reason + "; a: 1; b: 2"},
		"a bare code takes the note after a colon":           {&bare, 1, []string{note}, bare + ": " + note},
		"a Risk code alone takes the note after a colon":     {&risk, 2, []string{note}, risk + ": " + note},
		"a Risk code with detail takes it after a semicolon": {&riskDetail, 2, []string{note}, riskDetail + "; " + note},
		"a bare Risk rejection stays out of the Risk codes":  {&riskBare, 2, []string{note}, riskBare + "; " + note},
	} {
		t.Run(name, func(t *testing.T) {
			got := decisionnote.Append(tc.reason, 255, tc.codes, tc.notes...)
			if got == nil || *got != tc.want {
				t.Fatalf("Append = %v, want %q", got, tc.want)
			}
		})
	}
	if got := decisionnote.Append(nil, 255, 1); got != nil {
		t.Errorf("Append(nil) without notes = %q, want nil", *got)
	}
}

// The calibration note says the decision was judged on the all-versions total.
func TestFor_CalibrationNoteSaysItWasJudgedOnTheAllVersionsTotal(t *testing.T) {
	detail := "live version trader-v4 has fewer than 20 labeled samples in the confidence bucket"
	notes := decisionnote.For(1, "7203", true, true, detail, "", false)
	want := "calibration_all_versions: judged on all-versions total; " + detail
	if len(notes) != 1 || notes[0] != want {
		t.Fatalf("notes = %q, want [%q]", notes, want)
	}
	if got := decisionnote.For(1, "7203", false, true, detail, "", false); len(got) != 0 {
		t.Errorf("outside the bootstrap rule notes = %q, want none", got)
	}
}

// The column is 255 bytes wide: the reason gives way before a note does, and the
// result is cut on a rune boundary.
func TestAppend_KeepsTheNoteAndTheColumnWidth(t *testing.T) {
	reason := "meta_skip: " + strings.Repeat("障", 100)
	note := decisionnote.Note(decisionnote.MetaModelCohortMismatch, "meta model lr-1 was trained on cohort entry-quality-v3")
	got := decisionnote.Append(&reason, 255, 1, note)
	if got == nil || len(*got) > 255 || !strings.HasSuffix(*got, note) || !strings.Contains(*got, "…; ") {
		t.Fatalf("Append = %v (%d bytes), want <= 255 bytes ending with the whole note after a truncated reason", got, len(*got))
	}
	long := strings.Repeat("x", 400)
	if got := decisionnote.Append(nil, 255, 1, long); len(*got) > 255 {
		t.Errorf("a note longer than the column is %d bytes, want <= 255", len(*got))
	}
}

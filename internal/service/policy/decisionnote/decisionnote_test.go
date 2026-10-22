package decisionnote_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/policy/decisionnote"
)

func TestAppend(t *testing.T) {
	reason := "meta_skip: ev_not_above_minimum"
	note := decisionnote.Note(decisionnote.CalibrationAllVersions, "live version trader-v4 has fewer than 20 labeled samples in the confidence bucket")
	for name, tc := range map[string]struct {
		reason *string
		notes  []string
		want   string
	}{
		"no notes leaves the reason":          {&reason, nil, reason},
		"a note follows the reason":           {&reason, []string{note}, reason + "; " + note},
		"a passed signal gets the note alone": {nil, []string{note}, note},
		"an empty reason gets the note alone": {new(string), []string{note}, note},
		"two notes":                           {&reason, []string{"a: 1", "b: 2"}, reason + "; a: 1; b: 2"},
	} {
		t.Run(name, func(t *testing.T) {
			got := decisionnote.Append(tc.reason, 255, tc.notes...)
			if got == nil || *got != tc.want {
				t.Fatalf("Append = %v, want %q", got, tc.want)
			}
		})
	}
	if got := decisionnote.Append(nil, 255); got != nil {
		t.Errorf("Append(nil) without notes = %q, want nil", *got)
	}
}

// The column is 255 bytes wide: the reason gives way before a note does, and the
// result is cut on a rune boundary.
func TestAppend_KeepsTheNoteAndTheColumnWidth(t *testing.T) {
	reason := strings.Repeat("障", 100)
	note := decisionnote.Note(decisionnote.MetaModelCohortMismatch, "meta model lr-1 was trained on cohort entry-quality-v3")
	got := decisionnote.Append(&reason, 255, note)
	if got == nil || len(*got) > 255 || !strings.HasSuffix(*got, note) || !strings.Contains(*got, "…; ") {
		t.Fatalf("Append = %v (%d bytes), want <= 255 bytes ending with the whole note after a truncated reason", got, len(*got))
	}
	long := strings.Repeat("x", 400)
	if got := decisionnote.Append(nil, 255, long); len(*got) > 255 {
		t.Errorf("a note longer than the column is %d bytes, want <= 255", len(*got))
	}
}

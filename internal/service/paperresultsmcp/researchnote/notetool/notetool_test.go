package notetool

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce query.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a coded error", err)
	}
	return ce.ErrorCode()
}

func TestSpecsAreTheFourToolsAndOnlyTheCreateAndUpdateWrite(t *testing.T) {
	writes := map[string]bool{}
	for _, s := range Specs() {
		writes[s.Name] = s.Writes
		if s.Schema["additionalProperties"] != false || s.Description == "" {
			t.Errorf("%s: schema or description is not strict / empty", s.Name)
		}
	}
	want := map[string]bool{"research_note_create": true, "research_note_update": true, "research_notes_list": false, "research_note_get": false}
	if len(writes) != len(want) {
		t.Fatalf("tools = %v", writes)
	}
	for name, w := range want {
		if got, ok := writes[name]; !ok || got != w {
			t.Errorf("%s writes = %v (present %v), want %v", name, got, ok, w)
		}
	}
}

func TestDecodeRefusesBadArgumentsWithTheSpecCode(t *testing.T) {
	by := map[string]Spec{}
	for _, s := range Specs() {
		by[s.Name] = s
	}
	for name, c := range map[string]struct{ tool, args, code string }{
		"unknown field":       {"research_note_get", `{"note_id":1,"x":2}`, "invalid_argument"},
		"trailing data":       {"research_note_get", `{"note_id":1} {}`, "invalid_argument"},
		"no id":               {"research_note_get", `{}`, "invalid_argument"},
		"no question":         {"research_note_create", `{"hypothesis":"h"}`, "invalid_argument"},
		"no expected version": {"research_note_update", `{"note_id":1}`, "invalid_argument"},
		"limit over max":      {"research_notes_list", `{"limit":101}`, "limit_exceeded"},
		"limit zero":          {"research_notes_list", `{"limit":0}`, "invalid_argument"},
		"bad conclusion":      {"research_notes_list", `{"conclusion":"x"}`, "invalid_argument"},
	} {
		_, err := by[c.tool].Decode(json.RawMessage(c.args))
		if err == nil || codeOf(t, err) != c.code {
			t.Errorf("%s: err = %v, want code %s", name, err, c.code)
		}
	}
}

func TestFailureOfMapsTheNoteErrors(t *testing.T) {
	for want, err := range map[string]error{
		"research_note_version_conflict": &researchnote.ConflictError{Current: 3},
		"research_note_not_found":        researchnote.ErrNotFound,
		"write_disabled":                 mcpset.ErrWriteDisabled,
		"invalid_argument":               &researchnote.InvalidError{Reason: "r"},
	} {
		if got := codeOf(t, failureOf(err)); got != want {
			t.Errorf("failureOf(%v) code = %s, want %s", err, got, want)
		}
	}
	var ce query.CodedError
	if !errors.As(failureOf(&researchnote.ConflictError{Current: 3}), &ce) || ce.ErrorDetail() != "current_version は 3 です" {
		t.Error("the conflict does not name the current version")
	}
	other := errors.New("db busy")
	if failureOf(other) != other {
		t.Error("an unrelated error was changed")
	}
}

func TestResolvePeriodIsTheSharedJSTRule(t *testing.T) {
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC) // 2026-10-18 12:00 JST
	s := func(v string) *string { return &v }
	from, to, err := resolvePeriod(s("2026-10-15"), s("2026-10-16"), now)
	if err != nil || !from.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, query.JST)) || !to.Equal(time.Date(2026, 10, 16, 0, 0, 0, 0, query.JST)) {
		t.Fatalf("range = %v %v (%v)", from, to, err)
	}
	for name, c := range map[string][2]*string{
		"from only":  {s("2026-10-15"), nil},
		"bad format": {s("10/15"), s("2026-10-16")},
		"to <= from": {s("2026-10-16"), s("2026-10-16")},
		"future":     {s("2026-10-18"), s("2026-10-20")},
		"too long":   {s("2025-01-01"), s("2026-10-18")},
	} {
		if _, _, err := resolvePeriod(c[0], c[1], now); err == nil || codeOf(t, err) != "invalid_period" {
			t.Errorf("%s: err = %v, want invalid_period", name, err)
		}
	}
}

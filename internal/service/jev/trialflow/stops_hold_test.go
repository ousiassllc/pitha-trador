package trialflow_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// A 402/403 stop is not re-derived from stored rows like a cost cap is, so it is
// also held in memory: a failing settings write must not let the next decision
// call the stopped arm again.
func TestPaymentRequiredAndForbiddenStopsHoldWhenTheRecordCannotBeWritten(t *testing.T) {
	for name, tc := range map[string]struct {
		status       int
		stopsOthers  bool
		otherCallsLe int // calls of the other arm after the first decision (arms run in parallel: a 402 may or may not land before its check)
	}{
		"402 stops every arm": {status: 402, stopsOthers: true, otherCallsLe: 1},
		"403 stops that arm":  {status: 403, stopsOthers: false, otherCallsLe: 1},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, metalabelsettings.ModePaper, nil)
			stopped := e.addArm("a1", fail(&jev.APIError{StatusCode: tc.status, Body: "no"}, 3000))
			other := e.addArm("a2", answer(okResp(), 3000))
			e.enable(defaultArm("a1"), defaultArm("a2"))
			e.settings.mu.Lock()
			e.settings.failSets = true
			e.settings.mu.Unlock()

			e.evaluate()
			first := other.calls()
			e.evaluate()
			if stopped.calls() != 1 {
				t.Errorf("the stopped arm was called %d times, want 1: the stop must hold in memory", stopped.calls())
			}
			if first > tc.otherCallsLe {
				t.Errorf("the other arm was called %d times for the first decision, want at most %d", first, tc.otherCallsLe)
			}
			if tc.stopsOthers && other.calls() != first {
				t.Errorf("the other arm was called again after a 402 (%d -> %d): the whole trial must stay stopped", first, other.calls())
			}
			if !tc.stopsOthers && other.calls() != first+1 {
				t.Errorf("the other arm was called %d times in all, want %d: a 403 stops that arm only", other.calls(), first+1)
			}
			if _, ok := e.get(jevtrialsettings.KeyHalt); ok {
				t.Error("no record could be written, yet one exists")
			}
		})
	}
}

// A failed READ of a stop record is not "no record": the arm is not called.
func TestAFailedReadOfTheStopRecordsDoesNotCallTheArm(t *testing.T) {
	for _, key := range []string{jevtrialsettings.HaltKey("a1"), jevtrialsettings.FailuresKey("a1"), jevtrialsettings.ThrottleKey("a1"), jevtrialsettings.KeyHalt} {
		t.Run(key, func(t *testing.T) {
			e := newEnv(t, metalabelsettings.ModePaper, nil)
			a1 := e.addArm("a1", answer(okResp(), 3000))
			e.enable(defaultArm("a1"))
			e.settings.mu.Lock()
			e.settings.failGets = map[string]bool{key: true}
			e.settings.mu.Unlock()

			e.evaluate()
			if a1.calls() != 0 || len(e.rows()) != 0 {
				t.Fatalf("calls=%d rows=%d, want 0: the stop records could not be read", a1.calls(), len(e.rows()))
			}
			e.settings.mu.Lock()
			e.settings.failGets = nil
			e.settings.mu.Unlock()
			e.evaluate()
			if a1.calls() != 1 {
				t.Errorf("calls=%d after the read works again, want 1", a1.calls())
			}
		})
	}
}

// A vendor that echoes the rejected credential in its error body must not get it
// stored in jev_trial_decisions.
func TestErrorExcerptRedactsCredentials(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.addArm("a1", fail(&jev.APIError{StatusCode: 401, Body: "invalid key sk-or-v1-abcdef0123456789 / Authorization: Bearer abc.def-123"}, 3000))
	e.enable(defaultArm("a1"))
	e.evaluate()
	got := e.rowOf("a1").ErrorExcerpt
	if strings.Contains(got, "sk-or-v1") || strings.Contains(got, "abc.def") || !strings.Contains(got, "[redacted]") {
		t.Errorf("error_excerpt = %q, want the key and the bearer token redacted", got)
	}
}

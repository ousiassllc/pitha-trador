package apierrortests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/apierror"
)

// Issue #912: a failed Jev Trader call is recorded as api_error with the kind of
// the failure right after the head, so the failures can be counted by kind. The
// failures here are real: the Client talks to an httptest server (or to nothing)
// and the signal is read back from the database the Handler wrote it to.
func TestHandleJob_RecordsTheKindOfAJevFailure(t *testing.T) {
	// release is closed at the end of the subtest that waits on it: the server
	// cannot notice the client hanging up while the handler has not read the
	// request.
	var release chan struct{} // the current subtest's
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	for _, tc := range []struct {
		name    string
		serve   http.HandlerFunc // nil: nothing listens
		breakDB bool             // the answer is read but cannot be recorded
		want    string
	}{
		{"5xx", status(http.StatusInternalServerError), false, apierror.HTTP5xx},
		{"overloaded 529", status(529), false, apierror.HTTP5xx},
		{"rate limited", status(http.StatusTooManyRequests), false, apierror.HTTP429},
		{"unauthorized", status(http.StatusUnauthorized), false, apierror.HTTP4xxOther},
		{"unprocessable", status(http.StatusUnprocessableEntity), false, apierror.HTTP4xxOther},
		{"not JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, false, apierror.InvalidResponse},
		{"answer outside the options", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(jevtest.Response("m", map[string]any{"direction": jevtest.Choice("MAYBE", 0.9)}))
		}, false, apierror.InvalidResponse},
		{"slow answer", func(http.ResponseWriter, *http.Request) { <-release }, false, apierror.Timeout},
		{"nothing listens", nil, false, apierror.Network},
		{"answer not recorded", jevtest.TraderHandler(passingTraderResponse(domain.JevDirectionLong)), true, apierror.PersistFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := "http://127.0.0.1:1"
			done := make(chan struct{})
			release = done
			if tc.serve != nil {
				server := httptest.NewServer(tc.serve)
				t.Cleanup(server.Close)
				t.Cleanup(func() { close(done) }) // runs before server.Close
				url = server.URL
			}
			f := newFixture(t, url, 200*time.Millisecond)
			if tc.breakDB {
				// The decision insert is the first write the Trader makes: a trigger
				// that rejects it leaves the snapshot and the signal writable.
				if _, err := f.db.Exec(`CREATE TRIGGER no_decisions BEFORE INSERT ON jev_decisions BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.run(t); err == nil {
				t.Fatal("HandleJob = nil, want the Jev failure returned")
			}
			s := f.onlySignal(t)
			if s.Direction != domain.JevDirectionNone || s.RiskPassed || s.RejectReason == nil {
				t.Fatalf("signal = %+v, want a NONE that did not pass Risk, with a reason", s)
			}
			if want := policy.ReasonAPIError + ": " + tc.want + " ("; !strings.HasPrefix(*s.RejectReason, want) {
				t.Errorf("reject_reason = %q, want prefix %q", *s.RejectReason, want)
			}
		})
	}
}

// Issue #912: a NONE answer from Jev is recorded as jev_none with the answer's
// confidence and regime, so it can be told from a call that failed; the head
// (the text before ':'), the direction and risk_passed are what they were.
func TestHandleJob_JevNoneCarriesTheAnswersConfidenceAndRegime(t *testing.T) {
	resp := passingTraderResponse(domain.JevDirectionNone)
	resp.Confidence, resp.Regime = 0.8125, domain.JevRegimeRange
	server := httptest.NewServer(jevtest.TraderHandler(resp))
	t.Cleanup(server.Close)
	f := newFixture(t, server.URL, 5*time.Second)

	if err := f.run(t); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	s := f.onlySignal(t)
	if s.Direction != domain.JevDirectionNone || s.RiskPassed || s.JevDecisionID == nil {
		t.Errorf("signal = %+v, want a NONE that did not pass Risk, tied to the Jev decision", s)
	}
	if s.RejectReason == nil || *s.RejectReason != "jev_none: confidence=0.8125 regime=RANGE" {
		t.Errorf("reject_reason = %v, want jev_none: confidence=0.8125 regime=RANGE", s.RejectReason)
	}
}

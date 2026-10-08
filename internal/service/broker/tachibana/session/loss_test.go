package session_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// expire makes the next CLMTest answer p_errno=2 (session closed) once.
func (r *rig) expire() {
	r.t.Helper()
	var once atomic.Bool
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMTest" && !once.Swap(true) {
			return http.StatusOK, tt.ControlError("2", "session closed")
		}
		return http.StatusOK, nil
	})
	var apiErr *tachibana.APIError
	if err := r.call(); !errors.As(err, &apiErr) || apiErr.Kind() != tachibana.KindSessionExpired {
		r.t.Fatalf("err = %v, want session expired", err)
	}
}

func TestLostSessionRelogsInWithMinimumIntervalAndFlagsContention(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}

	// First loss: waits the minimum interval, then logs in again (not a fight yet).
	n := r.clk.Created()
	r.expire()
	r.clk.WaitCreated(t, n)
	if r.logins() != 1 {
		t.Fatal("re-login must wait the minimum interval")
	}
	r.advance(session.MinReloginInterval)
	tt.Eventually(t, func() bool {
		s := r.a.Status()
		return !s.Failed() && s.LoggedInAt.Equal(tt.AtJST(2026, 10, 8, 10, 0, 30))
	})
	if r.logins() != 2 || r.countNotices(session.NoticeContention) != 0 {
		t.Fatalf("logins=%d contention notices=%d, a single loss is not contention", r.logins(), r.countNotices(session.NoticeContention))
	}

	// A second quick loss right after the re-login: someone else is logging in.
	n = r.clk.Created()
	r.expire()
	tt.Eventually(t, func() bool { return r.countNotices(session.NoticeContention) == 1 })
	r.clk.WaitCreated(t, n)
	r.advance(session.MinReloginInterval)
	tt.Eventually(t, func() bool { return r.a.Status().Issue == broker.SessionIssueSessionConflict })
	if st := r.a.Status(); st.Guidance == "" || r.logins() != 3 {
		t.Fatalf("status = %+v logins=%d, want rejected with guidance after 3 logins", st, r.logins())
	}

	// A third loss is still answered; the fourth within the hour hits the cap.
	n = r.clk.Created()
	r.expire()
	r.clk.WaitCreated(t, n)
	r.advance(session.MinReloginInterval)
	tt.Eventually(t, func() bool { return r.logins() == 4 })
	r.expire()
	tt.Eventually(t, func() bool { return r.a.Status().NextReauth.Equal(tt.AtJST(2026, 10, 9, 5, 35)) })
	r.clk.Advance(10 * time.Minute)
	if r.logins() != 4 {
		t.Fatalf("logins = %d: past the cap the session must wait for the daily re-login", r.logins())
	}
	if r.countNotices(session.NoticeContention) != 1 {
		t.Errorf("contention notices = %d, want 1 per streak", r.countNotices(session.NoticeContention))
	}
	r.fb.Respond(nil)
	r.clk.Set(tt.AtJST(2026, 10, 9, 5, 35))
	tt.Eventually(t, func() bool { return r.logins() == 5 })
}

func TestContentionClearsOnceTheSessionSurvives(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	// Two quick losses in a row flag the fight.
	for range 2 {
		n := r.clk.Created()
		r.expire()
		r.clk.WaitCreated(t, n)
		r.advance(session.MinReloginInterval)
		tt.Eventually(t, func() bool { return !r.a.Status().Failed() || r.a.Status().Issue == broker.SessionIssueSessionConflict })
	}
	tt.Eventually(t, func() bool { return r.a.Status().Issue == broker.SessionIssueSessionConflict })

	r.fb.Respond(nil)
	r.clk.Advance(session.ContentionWindow + 1)
	if err := r.call(); err != nil {
		t.Fatal(err)
	}
	if st := r.a.Status(); st.Failed() {
		t.Errorf("status = %+v, the flag must clear after a request succeeds beyond the window", st)
	}
}

func TestClockSkewShowsGuidanceUntilARequestSucceeds(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	r.fb.Respond(func(tt.Request, int) (int, map[string]any) { return http.StatusOK, tt.ControlError("8", "time skew") })
	_ = r.call()
	st := r.a.Status()
	if st.Issue != broker.SessionIssueClockSkew || st.Code != tachibana.ErrnoClock || st.Guidance != session.GuidanceClock {
		t.Fatalf("status = %+v, want the NTP guidance", st)
	}
	r.fb.Respond(nil)
	if err := r.call(); err != nil {
		t.Fatal(err)
	}
	if r.a.Status().Failed() {
		t.Error("the guidance must clear after a request succeeds")
	}
}

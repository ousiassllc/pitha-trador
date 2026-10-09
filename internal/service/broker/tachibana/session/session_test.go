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

func TestStartLogsInAndLogsOutOnShutdown(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 6, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	st := r.a.Status()
	if st.Failed() || !st.LoggedInAt.Equal(tt.AtJST(2026, 10, 8, 6, 0)) || st.APIVersion != "e_api_v4r10" {
		t.Fatalf("status = %+v", st)
	}
	if want := tt.AtJST(2026, 10, 9, 5, 35); !st.NextReauth.Equal(want) {
		t.Errorf("NextReauth = %s, want %s", st.NextReauth, want)
	}
	if err := r.call(); err != nil {
		t.Fatal(err)
	}

	r.cancel()
	r.a.Run(r.ctx) // returns once the loop has logged out
	if got := r.fb.Count("CLMAuthLogoutRequest"); got != 1 {
		t.Fatalf("logout requests = %d, want 1", got)
	}
	reqs := r.fb.Requests()
	if last := reqs[len(reqs)-1]; last.Body["sCLMID"] != "CLMAuthLogoutRequest" || last.Path != "/"+tt.SecretTag+"/req/1" {
		t.Errorf("last request = %s %v: logout must go to the REQUEST URL", last.Path, last.Body)
	}
	if err := r.call(); !errors.Is(err, broker.ErrNoSession) {
		t.Errorf("after logout err = %v, want ErrNoSession", err)
	}
}

// The day's login is reused: nothing logs in again while the session holds.
func TestNoRelogInDuringTheDay(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 6, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	for range 10 { // 06:00 → 16:00
		r.clk.Advance(time.Hour)
		if err := r.call(); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.logins(); got != 1 {
		t.Errorf("logins = %d, want 1: re-login is only for the morning and for lost sessions", got)
	}
}

// 03:30 close → out_of_hours until the 05:35 re-login → business as usual.
func TestNightlyCloseThenMorningReauth(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 20, 0))
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	if err := r.call(); err != nil {
		t.Fatal(err)
	}

	r.advance(7*time.Hour + 30*time.Minute) // 03:30 close
	st := r.a.Status()
	if st.Issue != broker.SessionIssueOutOfHours || st.Guidance == "" {
		t.Fatalf("status at 03:30 = %+v, want out_of_hours", st)
	}
	if err := r.call(); !errors.Is(err, broker.ErrNoSession) {
		t.Fatalf("call at night err = %v, want ErrNoSession", err)
	}
	if got := r.a.BoardFailures().ConsecutiveFailures(); got != 0 {
		t.Errorf("market_data_down streak = %d at night, want 0", got)
	}

	r.clk.Advance(2 * time.Hour) // 05:30
	r.clk.Advance(4 * time.Minute)
	if got := r.logins(); got != 1 {
		t.Fatalf("logins at 05:34 = %d, want 1 (re-login is due at 05:35)", got)
	}
	r.clk.Advance(time.Minute) // 05:35
	tt.Eventually(t, func() bool {
		s := r.a.Status()
		return !s.Failed() && s.LoggedInAt.Equal(tt.AtJST(2026, 10, 9, 5, 35))
	})
	if got := r.logins(); got != 2 {
		t.Fatalf("logins = %d, want 2", got)
	}
	if err := r.call(); err != nil {
		t.Fatalf("call after the morning re-login: %v", err)
	}
	reqs := r.fb.Requests()
	relogin := reqs[len(reqs)-2]
	if relogin.Body["sCLMID"] != "CLMAuthLoginRequest" || relogin.Body["p_no"] != "1" || relogin.Body["p_sd_date"] != "2026.10.09-05:35:00.000" {
		t.Errorf("re-login request = %v: p_no must restart at 1 with the JST time", relogin.Body)
	}
	if last := reqs[len(reqs)-1]; last.Body["p_no"] != "2" || last.Path != "/"+tt.SecretTag+"/price/2" {
		t.Errorf("first request after re-login = %s %v, want p_no 2 on the new URL", last.Path, last.Body)
	}
}

func TestReauthTimeIsConfigurable(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 20, 0))
	settings := r.fb.Settings()
	settings.ReauthTime = "06:10"
	r.a = newAdapterWith(r, settings)
	if err := r.start(); err != nil {
		t.Fatal(err)
	}
	r.advance(7*time.Hour + 30*time.Minute)
	r.clk.Advance(2*time.Hour + 39*time.Minute) // 06:09
	if r.logins() != 1 {
		t.Fatal("re-logged in before the configured time")
	}
	r.clk.Advance(time.Minute)
	tt.Eventually(t, func() bool {
		s := r.a.Status()
		return !s.Failed() && s.LoggedInAt.Equal(tt.AtJST(2026, 10, 9, 6, 10))
	})
}

func TestFailedMorningLoginBacksOffAndNotifiesAfterDeadline(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 9, 5, 35))
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMAuthLoginRequest" {
			return http.StatusOK, tt.ControlError("-12", "stopped")
		}
		return http.StatusOK, nil
	})
	err := r.start()
	var apiErr *tachibana.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Start err = %v, want the *APIError", err)
	}
	st := r.a.Status()
	if st.Issue != broker.SessionIssueUnreachable || st.Failures != 1 || st.Guidance == "" {
		t.Fatalf("status = %+v", st)
	}

	wait := func() time.Duration { return r.a.Status().NextReauth.Sub(r.clk.Now()) }
	for i, want := range []time.Duration{5, 10, 20, 40, 80, 160, 300, 300} {
		want *= time.Second
		if got := wait(); got != want {
			t.Fatalf("after failure %d the next attempt is in %v, want %v", i+1, got, want)
		}
		r.advance(want)
		if got := r.a.Status().Failures; got != i+2 {
			t.Fatalf("failures = %d, want %d", got, i+2)
		}
	}
	if n := r.countNotices(session.NoticeLoginOverdue); n != 0 {
		t.Fatalf("overdue notice before 08:30: %d", n)
	}

	r.clk.Set(tt.AtJST(2026, 10, 9, 8, 31)) // the pending retry fires after the deadline
	tt.Eventually(t, func() bool { return r.countNotices(session.NoticeLoginOverdue) == 1 })
	r.advance(5 * time.Minute)
	r.advance(5 * time.Minute)
	if n := r.countNotices(session.NoticeLoginOverdue); n != 1 {
		t.Errorf("overdue notices = %d, want exactly 1 per outage", n)
	}

	r.fb.Respond(nil) // the broker is back
	r.advance(5 * time.Minute)
	tt.Eventually(t, func() bool { s := r.a.Status(); return !s.Failed() && s.Failures == 0 })
	if err := r.call(); err != nil {
		t.Fatalf("call after recovery: %v", err)
	}
}

func TestLoginInClosedWindowWaitsForOpening(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 9, 4, 0))
	var closed atomic.Bool
	closed.Store(true)
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMAuthLoginRequest" && closed.Load() {
			return http.StatusOK, tt.ControlError("-62", "closed")
		}
		return http.StatusOK, nil
	})
	if err := r.start(); err == nil {
		t.Fatal("login during the closed window must fail")
	}
	st := r.a.Status()
	if st.Issue != broker.SessionIssueOutOfHours || st.Code != tachibana.ErrnoOutOfHours {
		t.Fatalf("status = %+v, want out_of_hours/-62", st)
	}
	if got := st.NextReauth; !got.Equal(tt.AtJST(2026, 10, 9, 5, 30)) {
		t.Errorf("next attempt = %s, want 05:30 when the broker opens", got)
	}
	if got := r.a.BoardFailures().ConsecutiveFailures(); got != 0 {
		t.Errorf("market_data_down streak = %d, -62 must not count", got)
	}
	r.clk.Advance(89 * time.Minute) // 05:29
	if r.logins() != 1 {
		t.Fatal("retried before the broker opened")
	}
	closed.Store(false)
	r.clk.Advance(time.Minute) // 05:30
	tt.Eventually(t, func() bool { return !r.a.Status().Failed() })
	if r.countNotices(session.NoticeLoginOverdue) != 0 {
		t.Error("an overdue notice was raised for the closed window")
	}
}

func TestClassifyLoginFailures(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		issue broker.SessionIssue
		code  int
	}{
		{"wrong auth id", &tachibana.APIError{ResultCode: 10031}, broker.SessionIssueBadAuthID, 10031},
		{"auth id invalid", &tachibana.APIError{ResultCode: 10008}, broker.SessionIssueBadAuthID, 10008},
		{"login locked", &tachibana.APIError{ResultCode: 10033}, broker.SessionIssueBadAuthID, 10033},
		{"IP refused (IPv6)", &tachibana.APIError{ResultCode: 10005}, broker.SessionIssueIPRejected, 10005},
		{"passkey", &tachibana.APIError{ResultCode: 10063}, broker.SessionIssueAPIDisabled, 10063},
		{"closed", &tachibana.APIError{Errno: -62}, broker.SessionIssueOutOfHours, -62},
		{"clock", &tachibana.APIError{Errno: 8}, broker.SessionIssueClockSkew, 8},
		{"congested", &tachibana.APIError{Errno: -2}, broker.SessionIssueUnreachable, -2},
		{"other business error", &tachibana.APIError{ResultCode: 99999}, broker.SessionIssueUnknown, 99999},
		{"http 500", &tachibana.HTTPStatusError{Status: 500}, broker.SessionIssueUnreachable, 500},
		{"unreadable key", tachibana.ErrKeyUnreadable, broker.SessionIssueKeyMismatch, 0},
		{"wrong key", tachibana.ErrDecryptURL, broker.SessionIssueKeyMismatch, 0},
		{"documents", tachibana.ErrDocumentsUnread, broker.SessionIssueDocumentsUnread, 0},
		{"unexpected", errors.New("boom"), broker.SessionIssueUnknown, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue, code, guidance := session.Classify(tc.err)
			if issue != tc.issue || code != tc.code || guidance == "" {
				t.Errorf("classify = %q/%d/%q, want issue %q code %d with guidance", issue, code, guidance, tc.issue, tc.code)
			}
		})
	}
}

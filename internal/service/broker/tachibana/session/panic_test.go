package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/guard"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// panicLogin starts the rig, arms n panics for the loop's logins and makes
// the broker report the session lost, so the loop's next login panics: the
// first panic has happened once this returns.
func (r *rig) panicLogin(n int) {
	r.t.Helper()
	if err := r.start(); err != nil {
		r.t.Fatal(err)
	}
	created := r.clk.Created()
	r.expire()
	r.clk.WaitCreated(r.t, created) // the loop waits out the minimum re-login interval
	r.pc.Arm(n)
	created = r.clk.Created()
	r.clk.Advance(session.MinReloginInterval)
	r.clk.WaitCreated(r.t, created) // the restart delay was armed after the panic
	tt.Eventually(r.t, func() bool { return r.pc.Fired() == 1 })
}

// waitRestartArmed waits until the loop has armed the delay before the next
// restart (a timer created after created was read). The panic is counted
// before that timer exists, and advancing the clock in between would fire
// nothing. The panic of the last restart ends the loop: no timer follows it.
func (r *rig) waitRestartArmed(created, restart int) {
	r.t.Helper()
	if restart < guard.MaxRestarts {
		r.clk.WaitCreated(r.t, created)
	}
}

// restartBackoff is the delay before restart number n (1-based).
func restartBackoff(n int) time.Duration {
	d := guard.BackoffMin
	for i := 1; i < n; i++ {
		d = min(d*2, guard.BackoffMax)
	}
	return d
}

// A panic in the session loop is contained: the process (this test binary)
// lives, and the loop logs in again and carries on.
func TestSessionLoopPanicIsContainedAndLogsInAgain(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	r.panicLogin(1)
	if r.logins() != 1 {
		t.Fatalf("logins = %d, the panicking login must not reach the broker", r.logins())
	}

	r.clk.Advance(restartBackoff(1))
	tt.Eventually(t, func() bool { return r.logins() == 2 })
	tt.Eventually(t, func() bool { return !r.a.Status().Failed() })
	if err := r.call(); err != nil {
		t.Fatalf("call after the restart: %v", err)
	}
	if r.stops.Load() != 0 || r.countNotices(session.NoticeStopped) != 0 {
		t.Error("one panic must not stop the connection")
	}
}

// The loop is started again up to guard.MaxRestarts times, after the
// doubling delay each, and is not stopped before the last of them panicked.
func TestSessionLoopPanicRetriesUpToTheLimit(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	r.panicLogin(guard.MaxRestarts + 1)

	for restart := 1; restart <= guard.MaxRestarts; restart++ {
		d := restartBackoff(restart)
		r.clk.Advance(d - time.Nanosecond)
		if got := r.pc.Fired(); got != restart {
			t.Fatalf("restart %d came %v early: panics = %d", restart, time.Nanosecond, got)
		}
		if r.a.Status().Issue == broker.SessionIssueStopped {
			t.Fatalf("stopped before restart %d", restart)
		}
		created := r.clk.Created()
		r.clk.Advance(time.Nanosecond)
		want := restart + 1
		tt.Eventually(t, func() bool { return r.pc.Fired() == want })
		r.waitRestartArmed(created, restart)
	}
	if got := r.pc.Fired(); got != guard.MaxRestarts+1 {
		t.Errorf("panics = %d, want 1 + %d restarts", got, guard.MaxRestarts)
	}
}

// Past the limit only the 立花 connection stops: the status and a notice say
// so, the goroutine ends, nothing logs in again, and the caller is told.
func TestSessionLoopStopsPastTheLimit(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 10, 0))
	r.panicLogin(guard.MaxRestarts + 1)
	for restart := 1; restart <= guard.MaxRestarts; restart++ {
		created := r.clk.Created()
		r.clk.Advance(restartBackoff(restart))
		want := restart + 1
		tt.Eventually(t, func() bool { return r.pc.Fired() == want })
		r.waitRestartArmed(created, restart)
	}

	tt.Eventually(t, func() bool { return r.a.Status().Issue == broker.SessionIssueStopped })
	tt.Eventually(t, func() bool { return r.stops.Load() == 1 })
	if r.countNotices(session.NoticeStopped) != 1 {
		t.Errorf("stopped notices = %d, want 1", r.countNotices(session.NoticeStopped))
	}
	done := make(chan struct{})
	go func() { r.a.Session().Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session goroutine did not end")
	}

	logins := r.logins()
	r.clk.Advance(48 * time.Hour) // past the next close and daily re-login time
	if r.logins() != logins || r.pc.Fired() != guard.MaxRestarts+1 {
		t.Errorf("logins %d→%d, panics = %d: a stopped session must not run again", logins, r.logins(), r.pc.Fired())
	}
	if err := r.a.Session().LoginNow(r.ctx); err == nil {
		t.Error("LoginNow succeeded on a stopped session")
	}
	if err := r.call(); !errors.Is(err, broker.ErrNoSession) {
		t.Errorf("request on a stopped session = %v, want ErrNoSession", err)
	}
	if st := r.a.Status(); st.Guidance == "" || !st.Failed() {
		t.Errorf("status = %+v, want a failed state with guidance", st)
	}
}

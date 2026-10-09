package event_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func rows(m map[int]map[string]string) map[int]map[string]string { return m }

func TestConnectsWithTheManualsQueryAndMergesFDIntoLatest(t *testing.T) {
	r := newRig(t, 10, 60)
	if err := r.feed.SetWatch(r.ctx, []string{"7203", "6758", "7203", "bad code!", ""}); err != nil {
		t.Fatal(err)
	}
	r.run()
	conn := r.fb.WaitEvent(t, 1)

	if !strings.Contains(conn.Path, "/"+tt.SecretTag+"/ws/1") {
		t.Errorf("path = %q, want the decrypted EVENT-WebSocket URL", conn.Path)
	}
	want := map[string]string{
		"p_rid": "22", "p_board_no": "1000", "p_gyou_no": "1,2", "p_issue_code": "7203,6758", "p_mkt_code": "00,00",
		"p_eno": "0", "p_evt_cmd": "ST,KP,FD,EC,SS,US",
	}
	for k, v := range want {
		if conn.Query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, conn.Query.Get(k), v)
		}
	}
	if !strings.Contains(conn.RawQuery, "p_gyou_no=1,2&") {
		t.Errorf("raw query = %q, want unescaped commas as in the manual", conn.RawQuery)
	}

	// First FD: the memory snapshot; then a difference touching row 1 only.
	_ = conn.Send(tt.FD(1, rows(map[int]map[string]string{
		1: {"pDPP": "3000", "tDPP:T": "10:00", "pQBP": "2999", "pBV": "100", "pQAP": "3001", "pAV": "200", "pQAS": "0101", "pQBS": "0101", "pDV": "5000"},
		2: {"pDPP": "1500", "pQBP": "1499", "pQAP": "1501"},
	})))
	_ = conn.Send(tt.FD(2, rows(map[int]map[string]string{1: {"pDPP": "3005", "pQAS": "0102"}})))
	_ = conn.Send(tt.SS(3, 1, true)) // processed after both FDs: the marker that they are merged
	tt.Eventually(t, func() bool { return r.feed.Statuses().SystemKnown })

	q, err := r.feed.Latest(context.Background(), "7203")
	if err != nil || q.Price != 3005 {
		t.Fatalf("Latest = %+v, %v", q, err)
	}
	if *q.Bid != 2999 || *q.Ask != 3001 || *q.BidQty != 100 || q.Volume != 5000 || !q.SpecialQuote {
		t.Errorf("merged quote = %+v, want the snapshot's items kept and the difference applied", q)
	}
	if q2, err := r.feed.Latest(context.Background(), "6758"); err != nil || q2.Price != 1500 {
		t.Errorf("6758 = %+v, %v", q2, err)
	}
	if n := r.restRefills(); n != 0 {
		t.Errorf("REST refills = %d, want none while EVENT values are fresh", n)
	}
	if got := fmt.Sprint(r.feed.Subscribed()); got != "[7203 6758]" {
		t.Errorf("Subscribed = %s", got)
	}
}

// KP silence, an ST other than a lost session, and a dropped connection all
// reconnect to the same virtual URL without a login, carrying p_eno.
func TestReconnectsToTheSameURLWithoutRelogin(t *testing.T) {
	cases := []struct {
		name string
		kill func(r *rig, c *tt.EventConn)
	}{
		{"keep-alive silence", func(r *rig, _ *tt.EventConn) {
			r.advanceUntil(func() bool { return len(r.fb.Events()) >= 2 })
		}},
		{"ST busy", func(_ *rig, c *tt.EventConn) { _ = c.Send(tt.ST(9, -2, "busy")); c.Close() }},
		{"ST service stopped", func(_ *rig, c *tt.EventConn) { _ = c.Send(tt.ST(9, 9, "offline")); c.Close() }},
		{"connection dropped", func(_ *rig, c *tt.EventConn) { c.Drop() }},
		{"closed by the broker", func(_ *rig, c *tt.EventConn) { c.Close() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, 10, 60)
			_ = r.feed.SetWatch(r.ctx, []string{"7203"})
			r.run()
			c1 := r.fb.WaitEvent(t, 1)
			_ = c1.Send(tt.SS(1, 42, true)) // an event number to resume after
			tt.Eventually(t, func() bool { return r.feed.Statuses().SystemKnown })

			tc.kill(r, c1)
			r.advanceUntil(func() bool { return len(r.fb.Events()) >= 2 })
			c2 := r.fb.Events()[1]

			if c2.Path != c1.Path {
				t.Errorf("reconnected to %q, want the same virtual URL %q", c2.Path, c1.Path)
			}
			if got := c2.Query.Get("p_eno"); got != "42" {
				t.Errorf("p_eno = %q, want 42 (resume after the last event number)", got)
			}
			if n := r.fb.Count("CLMAuthLoginRequest"); n != 1 {
				t.Errorf("logins = %d, want no re-login (the same virtual URL is reused)", n)
			}
			if r.feed.ConnectionsToday() != 2 {
				t.Errorf("connections today = %d, want 2 (recovery reconnects are counted)", r.feed.ConnectionsToday())
			}
		})
	}
}

func TestSessionLostSTTriggersReloginAndReconnectsToTheNewURL(t *testing.T) {
	r := newRig(t, 10, 60)
	sess := newSession(t, r)
	if err := sess.Start(r.ctx); err != nil {
		t.Fatal(err)
	}
	_ = r.feed.SetWatch(r.ctx, []string{"7203"})
	r.run()
	c1 := r.fb.WaitEvent(t, 1)

	_ = c1.Send(tt.ST(5, 2, "session inactive."))
	c1.Close()
	r.advanceUntil(func() bool { return len(r.fb.Events()) >= 2 })

	c2 := r.fb.Events()[1]
	if !strings.Contains(c2.Path, "/ws/") || c2.Path == c1.Path {
		t.Errorf("second connection %q, want the new login's virtual URL (first was %q)", c2.Path, c1.Path)
	}
	// The harness's own login plus the one this loss caused.
	if n := r.fb.Count("CLMAuthLoginRequest"); n != 3 {
		t.Errorf("logins = %d, want 3 (test login, session start, re-login after p_errno=2)", n)
	}
}

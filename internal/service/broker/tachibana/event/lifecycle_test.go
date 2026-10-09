package event_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"

	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestRecordsOrdersAndStatusesAndDecodesNames(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"2468"})
	r.run()
	c := r.fb.WaitEvent(t, 1)

	sjis, _ := japanese.ShiftJIS.NewEncoder().Bytes([]byte("フュートレック"))
	_ = c.Send(tt.EC(1, 10507, "3000945", "2468", base64.StdEncoding.EncodeToString(sjis)))
	_ = c.Send(tt.SS(2, 10600, false))
	_ = c.Send("p_no\x023\x01p_date\x02x\x01p_cmd\x02US\x01p_ENO\x0210700\x01p_MC\x0200\x01p_UU\x020101\x01p_US\x02100")

	tt.Eventually(t, func() bool { return len(r.feed.Orders()) == 1 && r.feed.Statuses().Operations["00/0101"] == "100" })
	o := r.feed.Orders()[0]
	if o.EventNo != 10507 || o.OrderNo != "3000945" || o.Symbol != "2468" || o.Name != "フュートレック" || o.NotifyType != "12" {
		t.Errorf("order = %+v", o)
	}
	if s := r.feed.Statuses(); !s.SystemKnown || s.SystemOpen {
		t.Errorf("statuses = %+v, want a known closed system", s)
	}
}

func TestNoConnectionWithoutSymbolsAndWaitsForTheSession(t *testing.T) {
	r := newRig(t, 10, 60)
	r.run()
	time.Sleep(50 * time.Millisecond)
	if n := len(r.fb.Events()); n != 0 {
		t.Fatalf("connections = %d, want none without a watch list", n)
	}
	_ = r.feed.SetWatch(r.ctx, []string{"7203"})
	r.fb.WaitEvent(t, 1)
}

// A new login (the old virtual URL is dead) moves the stream to the new URL at once.
func TestNewLoginMovesTheStreamToTheNewURL(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"7203"})
	r.run()
	c1 := r.fb.WaitEvent(t, 1)
	if _, err := r.c.Login(context.Background(), tt.AuthID, r.fb.Key()); err != nil {
		t.Fatal(err)
	}
	c2 := r.fb.WaitEvent(t, 2)
	if c2.Path == c1.Path {
		t.Errorf("still on %q after a new login", c2.Path)
	}
	select {
	case <-c1.Done():
	case <-time.After(5 * time.Second):
		t.Error("the old connection was not closed")
	}
}

// Neither the virtual URL, the 認証ID nor the key reaches a log line or an
// error, including the connection errors (the EVENT URL is in every dial).
func TestConnectionFailureNeverLeaksTheVirtualURL(t *testing.T) {
	logs := captureLogs(t)
	r := newRig(t, 10, 60)
	secrets := r.fb.Secrets()
	r.fb.Server().Close() // every dial now fails with a *url.Error carrying the URL
	_ = r.feed.SetWatch(r.ctx, []string{"7203"})
	r.run()
	tt.Eventually(t, func() bool { return strings.Contains(logs.String(), "EVENT connection ended") })
	for _, s := range secrets {
		if strings.Contains(logs.String(), s) {
			t.Errorf("log output contains %q:\n%s", s, logs.String())
		}
	}
	if strings.Contains(logs.String(), "ws/1") {
		t.Errorf("log output contains the virtual URL path:\n%s", logs.String())
	}
}

func TestPlannedSwapsAreBatchedAndLimitedByTheDailyBudget(t *testing.T) {
	logs := captureLogs(t)
	r := newRig(t, 3, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"1001"})
	r.run()
	r.fb.WaitEvent(t, 1)

	// Removals never reconnect.
	_ = r.feed.SetWatch(r.ctx, []string{"1001", "1002"})
	r.fb.WaitEvent(t, 2) // additions: one batched reconnect
	_ = r.feed.SetWatch(r.ctx, []string{"1002"})
	time.Sleep(50 * time.Millisecond)
	if n := len(r.fb.Events()); n != 2 {
		t.Fatalf("connections = %d, want 2: dropping a symbol must not reconnect", n)
	}

	// Two additions at once cost a single reconnect.
	_ = r.feed.SetWatch(r.ctx, []string{"1002", "1003", "1004"})
	c3 := r.fb.WaitEvent(t, 3)
	if got := c3.Query.Get("p_issue_code"); got != "1002,1003,1004" {
		t.Errorf("third connection watches %q", got)
	}
	if r.feed.ConnectionsToday() != 3 {
		t.Fatalf("connections today = %d, want 3", r.feed.ConnectionsToday())
	}

	// The budget (3) is used up: the next swap is suspended with a warning.
	_ = r.feed.SetWatch(r.ctx, []string{"1002", "1003", "1004", "1005"})
	tt.Eventually(t, func() bool { return strings.Contains(logs.String(), "swap suspended") })
	time.Sleep(50 * time.Millisecond)
	if n := len(r.fb.Events()); n != 3 {
		t.Fatalf("connections = %d, want 3: no swap past the budget", n)
	}

	// A recovery reconnect is never refused, counts, and picks the new symbol up.
	c3.Drop()
	r.advanceUntil(func() bool { return len(r.fb.Events()) >= 4 })
	if got := r.fb.Events()[3].Query.Get("p_issue_code"); got != "1002,1003,1004,1005" {
		t.Errorf("recovery connection watches %q, want the full wanted list", got)
	}
	if r.feed.ConnectionsToday() != 4 {
		t.Errorf("connections today = %d, want 4", r.feed.ConnectionsToday())
	}
	if !strings.Contains(logs.String(), "exceed the daily budget") {
		t.Error("exceeding the budget by a recovery reconnect must be logged")
	}

	// The tally restarts the next JST day.
	r.clk.Advance(24 * time.Hour)
	if n := r.feed.ConnectionsToday(); n != 0 {
		t.Errorf("connections the next day = %d, want 0", n)
	}
}

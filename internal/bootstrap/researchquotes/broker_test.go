package researchquotes

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// brokerRig is a Recorder over the real EVENT feed against the fake 立花 broker,
// on a manual clock.
type brokerRig struct {
	*rig
	clk  *tt.ManualClock
	fb   *tt.FakeBroker
	feed *event.Feed
}

// feedClock tells the feed the rig's time but arms its timers on a clock of its
// own that nothing advances. The feed's 15 s keep-alive watchdog and reconnect
// backoff are timers: on the rig's clock, the minutes the tests advance would
// fire the watchdog whenever the feed had connected by then (the fake broker
// sends no KP), and the feed would reconnect, which counted a second connection
// depending on goroutine timing. These tests are about the feed's REST interval
// and connection budget, which read the time, not about keep-alive.
type feedClock struct {
	now    *tt.ManualClock
	timers *tt.ManualClock
}

func (c feedClock) Now() time.Time { return c.now.Now() }

func (c feedClock) NewTimer(d time.Duration) tachibana.Timer { return c.timers.NewTimer(d) }

func newBrokerRig(t *testing.T, refill time.Duration, onQuote tt.Responder) *brokerRig {
	t.Helper()
	b := &brokerRig{rig: newRig(t), clk: tt.NewManualClock(thursday(9, 30, 5))}
	b.fb = tt.New(t, b.clk)
	if onQuote != nil {
		b.fb.Respond(onQuote)
	}
	c := tachibana.NewClient(tachibana.Config{BaseURL: b.fb.BaseURL(), RequestsPerSecond: 10, HTTPClient: b.fb.Server().Client(), Clock: b.clk})
	if _, err := c.Login(context.Background(), tt.AuthID, b.fb.Key()); err != nil {
		t.Fatal(err)
	}
	b.feed = event.New(event.Config{Client: c, ConnectionBudget: 10, RefillInterval: refill, HTTPClient: b.fb.Server().Client(), Clock: feedClock{now: b.clk, timers: tt.NewManualClock(thursday(9, 30, 5))}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); b.feed.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the feed did not stop")
		}
	})
	b.rec = New(Config{
		Settings:    func(context.Context) (tachibanasource.TachibanaSourceSettings, error) { return b.set, nil },
		Start:       func(context.Context) (Feed, error) { b.starts++; return b.feed, nil },
		KabuSymbols: func() []string { return b.kabu },
		Store:       researchquote.New(b.db),
		Status:      researchstatus.New(b.db),
		Clock:       b.clk,
	})
	return b
}

func (b *brokerRig) snapshotRows() int {
	b.t.Helper()
	var n int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM market_snapshots`).Scan(&n); err != nil {
		b.t.Fatal(err)
	}
	return n
}

// Switched on, the recorder subscribes kabu's symbols plus the extras and writes
// 立花's values into research_quote_samples only: kabu's watch list is read, not
// changed, and market_snapshots stays exactly as it was.
func TestRecordsTheEventFeedIntoResearchTableOnlyLeavingKabuAndSnapshotsUntouched(t *testing.T) {
	b := newBrokerRig(t, 300*time.Second, nil)
	inst, err := market.NewInstrumentRepository(b.db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.NewSnapshotRepository(b.db).Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: thursday(9, 29, 0), Price: 3000, RawDataJSON: "{}", Feature: domain.Feature{VWAP: 3000},
	}); err != nil {
		t.Fatal(err)
	}
	before := b.snapshotRows()
	b.kabu = []string{"7203", "6758"}
	kabuBefore := append([]string(nil), b.kabu...)
	b.set = settings("9984")

	b.tick(b.clk.Now()) // plans and subscribes: the feed connects
	conn := b.fb.WaitEvent(t, 1)
	if got := conn.Query.Get("p_issue_code"); got != "7203,6758,9984" {
		t.Fatalf("subscribed %q, want kabu's symbols first then the extra", got)
	}
	_ = conn.Send(tt.FD(1, rows3(map[int]map[string]string{
		1: {"pDPP": "3000", "pQBP": "2999", "pQAP": "3001"},
		2: {"pDPP": "1500"},
	})))
	_ = conn.Send(tt.SS(2, 1, true))
	tt.Eventually(t, func() bool { return b.feed.Statuses().SystemKnown })
	b.clk.Advance(55 * time.Second)

	b.tick(b.clk.Now())
	if got := b.rows(); got != 2 {
		t.Fatalf("research rows = %d, want 2 (9984 has no price yet and gets none)", got)
	}
	var via string
	if err := b.db.QueryRow(`SELECT DISTINCT via FROM research_quote_samples`).Scan(&via); err != nil || via != "event" {
		t.Errorf("via = %q (%v), want event", via, err)
	}
	if b.snapshotRows() != before {
		t.Errorf("market_snapshots changed: %d rows, was %d", b.snapshotRows(), before)
	}
	if len(b.kabu) != len(kabuBefore) || b.kabu[0] != kabuBefore[0] || b.kabu[1] != kabuBefore[1] {
		t.Errorf("kabu's symbols = %v, want untouched %v", b.kabu, kabuBefore)
	}
}

func rows3(m map[int]map[string]string) map[int]map[string]string { return m }

// Switch off or credentials unset: nothing reaches the broker, not even a login.
func TestSwitchOffAndUnsetCredentialsSendNoRequestToTheBroker(t *testing.T) {
	fb := tt.New(t, tt.NewManualClock(thursday(9, 30, 5)))
	for _, tc := range []struct {
		name string
		on   bool
		err  error
	}{
		{"switch off", false, nil},
		{"credentials unset", true, ErrNotConfigured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.set.Research.QuotesEnabled = tc.on
			r.kabu = []string{"7203"}
			r.rec.cfg.Start = func(context.Context) (Feed, error) {
				r.starts++
				return nil, tc.err // the hub refuses before any network access
			}
			for i := 0; i < 3; i++ {
				r.tick(thursday(9, 30+i, 5))
			}
			if !tc.on && r.starts != 0 {
				t.Errorf("Start called %d times with the switch off", r.starts)
			}
			if n := len(fb.Requests()) + len(fb.Events()); n != 0 {
				t.Errorf("the broker saw %d requests/connections, want none", n)
			}
		})
	}
}

// The feed's REST interval bounds the assist: a symbol without a value asks
// Latest every minute, but the broker is asked at most once per interval; and
// the feed's daily connection budget governs the subscription (one connection
// for any number of recorder cycles).
func TestRestAssistIsLimitedByTheFeedsIntervalAndSubscriptionIsOneConnection(t *testing.T) {
	quotes := 0
	b := newBrokerRig(t, 300*time.Second, func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		quotes++
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0", "aCLMMfdsMarketPrice": []map[string]string{}}
	})
	b.kabu = []string{"0001"} // the broker has no price for it
	for i := 0; i < 5; i++ {  // 09:30:05 .. 09:34:05
		b.tick(b.clk.Now())
		b.clk.Advance(time.Minute)
	}
	if quotes != 1 {
		t.Errorf("REST 時価 requests = %d after 5 minutes, want 1 (interval 300 s)", quotes)
	}
	b.tick(b.clk.Now()) // 09:35:05
	if quotes != 2 {
		t.Errorf("REST 時価 requests = %d at +300 s, want 2", quotes)
	}
	if n := b.rows(); n != 0 {
		t.Errorf("rows = %d, want none for a symbol without a price", n)
	}
	b.fb.WaitEvent(t, 1)
	if n := b.feed.ConnectionsToday(); n != 1 {
		t.Errorf("connections today = %d, want 1 for an unchanged plan", n)
	}
}

// Run ticks at the 5th second of each minute on the injected clock.
func TestRunTicksAtTheFifthSecondOfEachMinute(t *testing.T) {
	r := newRig(t)
	clk := tt.NewManualClock(thursday(9, 29, 30))
	r.rec.clock = clk
	r.kabu = []string{"7203"}
	r.feed.put("7203", 3000, thursday(9, 29, 0), false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.rec.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	clk.WaitCreated(t, 0)
	clk.Advance(34 * time.Second) // 09:30:04: not yet
	if r.rows() != 0 {
		t.Fatal("recorded before the 5th second")
	}
	clk.Advance(time.Second) // 09:30:05
	tt.Eventually(t, func() bool { return r.rows() == 1 })
	clk.WaitCreated(t, 1)
	clk.Advance(time.Minute) // 09:31:05
	tt.Eventually(t, func() bool { return r.rows() == 2 })
}

package disclosureskiptests

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// swappableFeed is a feed whose answer the test changes between cycles.
type swappableFeed struct {
	mu    sync.Mutex
	items map[string][]assist.NewsItem
	err   error
	calls map[string]int
}

func (f *swappableFeed) Fetch(_ context.Context, symbol string) ([]assist.NewsItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[symbol]++
	return f.items[symbol], f.err
}

func (f *swappableFeed) set(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *swappableFeed) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

// followupRig is a Followup and an Evaluator over one database, the Followup
// fetching through a RecordingFeed whose writer stores into that database, as
// the application wires them. The clock is shared and moved by the test.
type followupRig struct {
	t        *testing.T
	store    *disclosureskip.Store
	inner    *swappableFeed
	followup *disclosureskip.Followup
	eval     *disclosureskip.Evaluator
	now      time.Time
	written  chan struct{}
	// gate holds the writer back while the test holds its write lock: the
	// recording feed stores a fetch asynchronously, and this is a slow write.
	gate sync.RWMutex
}

func newFollowupRig(t *testing.T, start time.Time, items ...assist.NewsItem) (*followupRig, *sql.DB) {
	t.Helper()
	conn := openDB(t)
	r := &followupRig{t: t, store: disclosureskip.NewStore(conn), now: start, written: make(chan struct{}, 64)}
	r.inner = &swappableFeed{items: map[string][]assist.NewsItem{sym: items}}
	clock := func() time.Time { return r.now }
	sink := sinkFunc(func(ctx context.Context, ev disclosureskip.FetchEvent) error {
		r.gate.RLock()
		defer r.gate.RUnlock()
		err := r.store.WriteFetch(ctx, ev)
		r.written <- struct{}{}
		return err
	})
	recording := disclosureskip.NewRecordingFeed(r.inner, newsfeed.YanoshinLimit, sink, disclosureskip.WithClock(clock))
	runWriter(t, recording)
	r.followup = disclosureskip.NewFollowup(r.store, recording, weekdaysOnly, disclosureskip.WithFollowupClock(clock))
	r.eval = disclosureskip.NewEvaluator(r.store, weekdaysOnly, disclosureskip.WithEvaluatorClock(clock))
	return r, conn
}

// cycle runs one Followup cycle at at and waits for the writer to store the n
// fetches it made.
func (r *followupRig) cycle(at time.Time, n int) {
	r.t.Helper()
	r.now = at
	if err := r.followup.Cycle(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	for range n {
		waitFor(r.t, r.written)
	}
}

func (r *followupRig) evaluate(at time.Time) {
	r.t.Helper()
	r.now = at
	if err := r.eval.Cycle(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

// A symbol that dropped out of News Ingest's targets right after the decision
// gets one covering fetch from the Followup, and the decision ends as no_skip
// where, without it, it would end as unknown / no_fetch (cause 1 of #1053).
func TestFollowup_DroppedSymbolGetsACoveringFetchAndNoFetchDecreases(t *testing.T) {
	decided := jstAt(10, 14, 10, 0, 0)

	// Without the Followup: News Ingest's last fetch was inside the lag.
	control := openDB(t)
	controlStore := disclosureskip.NewStore(control)
	insertDecision(t, control, 1, "trader", "LONG", decided)
	if err := controlStore.WriteFetch(context.Background(), disclosureskip.FetchEvent{Symbol: sym, At: decided.Add(time.Minute), OK: true}); err != nil {
		t.Fatal(err)
	}
	controlEval := disclosureskip.NewEvaluator(controlStore, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return decided.Add(61 * time.Minute) }))
	if err := controlEval.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := recordOf(t, control, 1); !ok || r.verdict != "unknown" || r.reason != "no_fetch" {
		t.Fatalf("control record = %+v, %v; want unknown / no_fetch", r, ok)
	}

	// With it.
	rig, conn := newFollowupRig(t, decided)
	insertDecision(t, conn, 1, "trader", "LONG", decided)
	if err := rig.store.WriteFetch(context.Background(), disclosureskip.FetchEvent{Symbol: sym, At: decided.Add(time.Minute), OK: true}); err != nil {
		t.Fatal(err)
	}
	rig.cycle(decided.Add(6*time.Minute), 0) // inside lag + grace: News Ingest may still cover it
	rig.evaluate(decided.Add(6 * time.Minute))
	if got := rig.inner.total(); got != 0 {
		t.Fatalf("%d requests before lag+grace passed, want none", got)
	}
	rig.cycle(decided.Add(8*time.Minute), 1)
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("followup requests = %d, want 1", got)
	}
	rig.evaluate(decided.Add(8*time.Minute + time.Second))
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" {
		t.Fatalf("record = %+v, %v; want no_skip: the followup fetch covers the decision", r, ok)
	}
	// One request settles it: a decided decision causes no further fetch.
	rig.cycle(decided.Add(9*time.Minute), 0)
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("requests = %d after the decision was recorded, want still 1", got)
	}
}

// The Followup asks for nothing it does not need: no decision, no request (no
// polling of the universe, #531); a symbol News Ingest already covered is not
// fetched again; and nothing is fetched outside the trading session or after the
// give-up time.
func TestFollowup_FetchesOnlyWhatAWaitingDecisionNeeds(t *testing.T) {
	decided := jstAt(10, 14, 10, 0, 0)
	rig, conn := newFollowupRig(t, decided)

	rig.cycle(decided.Add(10*time.Minute), 0)
	if got := rig.inner.total(); got != 0 {
		t.Fatalf("%d requests without any decision, want none", got)
	}

	insertDecision(t, conn, 1, "trader", "LONG", decided)
	insertDecision(t, conn, 2, "trader", "NONE", decided)  // not an entry candidate
	insertDecision(t, conn, 3, "trader", "SHORT", decided) // the same symbol: one request
	// News Ingest covered it at t+6min already.
	if err := rig.store.WriteFetch(context.Background(), disclosureskip.FetchEvent{Symbol: sym, At: decided.Add(6 * time.Minute), OK: true}); err != nil {
		t.Fatal(err)
	}
	rig.cycle(decided.Add(10*time.Minute), 0)
	if got := rig.inner.total(); got != 0 {
		t.Fatalf("%d requests for a symbol News Ingest already covered, want none", got)
	}

	// After the give-up time (the decision is about to be recorded as unknown or
	// covered) there is no point fetching.
	other, otherConn := newFollowupRig(t, decided)
	insertDecision(t, otherConn, 1, "trader", "LONG", decided)
	other.cycle(decided.Add(61*time.Minute), 0)
	if got := other.inner.total(); got != 0 {
		t.Fatalf("%d requests after the give-up time, want none", got)
	}
}

// News Ingest does not poll in the lunch break, in the evening or on a holiday,
// and neither does the Followup: a decision at 11:25 is fetched for when the
// afternoon session opens, not at 11:40.
func TestFollowup_OnlyFetchesInTheTradingSession(t *testing.T) {
	decided := jstAt(10, 14, 11, 25, 0)
	rig, conn := newFollowupRig(t, decided)
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	rig.cycle(jstAt(10, 14, 11, 40, 0), 0) // lunch break
	rig.cycle(jstAt(10, 14, 12, 29, 0), 0) // still the lunch break
	if got := rig.inner.total(); got != 0 {
		t.Fatalf("%d requests in the lunch break, want none", got)
	}
	rig.cycle(jstAt(10, 14, 12, 31, 0), 1)
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("requests = %d at 12:31, want 1", got)
	}
	rig.evaluate(jstAt(10, 14, 12, 32, 0))
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" {
		t.Fatalf("record = %+v, %v; want the lunch-break-spanning decision covered by the afternoon fetch", r, ok)
	}

	// After the close: a decision at 15:27 is fetched for the next morning.
	closeAt := jstAt(10, 14, 15, 27, 0)
	insertDecision(t, conn, 2, "trader", "LONG", closeAt)
	rig.cycle(jstAt(10, 14, 15, 29, 0), 0) // before t+lag+grace and in session
	rig.cycle(jstAt(10, 14, 16, 0, 0), 0)  // after the close
	rig.cycle(jstAt(10, 15, 8, 59, 0), 0)  // before the open
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("requests = %d outside the session, want still 1", got)
	}
	rig.cycle(jstAt(10, 15, 9, 1, 0), 1)
	if got := rig.inner.total(); got != 2 {
		t.Fatalf("requests = %d at 09:01, want 2", got)
	}
}

// A failed fetch is recorded as a failure and is no coverage; once the feed
// recovers (and the guard's backoff, which makes no request and records
// nothing, has passed) the next fetch covers the decision.
func TestFollowup_FailedFetchIsNoCoverageAndARecoveredFetchCoversTheDecision(t *testing.T) {
	decided := jstAt(10, 14, 10, 0, 0)
	rig, conn := newFollowupRig(t, decided)
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	rig.inner.set(errors.New("yanoshin down"))
	rig.cycle(decided.Add(8*time.Minute), 1)
	rig.evaluate(decided.Add(9 * time.Minute))
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("a failed fetch finalized the decision before the give-up time")
	}
	var outcome string
	if err := conn.QueryRow(`SELECT outcome FROM disclosure_fetches`).Scan(&outcome); err != nil || outcome != "error" {
		t.Fatalf("recorded fetch outcome = %q, %v; want the failure recorded", outcome, err)
	}

	// Backing off: the guard returns ErrBackingOff without a request. Nothing is recorded.
	rig.inner.set(newsfeed.ErrBackingOff)
	rig.cycle(decided.Add(10*time.Minute), 0)
	if got := countRows(t, conn, "disclosure_fetches"); got != 1 {
		t.Fatalf("fetches = %d, want the backoff not recorded", got)
	}

	rig.inner.set(nil)
	rig.cycle(decided.Add(30*time.Minute), 1)
	rig.evaluate(decided.Add(31 * time.Minute))
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" {
		t.Fatalf("record = %+v, %v; want no_skip after the feed recovered", r, ok)
	}
}

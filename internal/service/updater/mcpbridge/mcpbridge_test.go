package mcpbridge_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/service/updater/mcpbridge"
)

type fakeChecker struct {
	mu     sync.Mutex
	status updater.Status
	result updater.Result
	err    error
	safe   bool
	checks int
}

func (f *fakeChecker) CheckForUpdate(context.Context) (updater.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.result, f.err
}

func (f *fakeChecker) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

func (f *fakeChecker) Status() updater.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeChecker) SafeToUpdate(context.Context) (bool, updater.BlockReason) {
	if f.safe {
		return true, updater.BlockReason{}
	}
	return false, updater.BlockReason{Kind: updater.BlockKillSwitch, Detail: "kill switch active"}
}

type fakeQuitter struct {
	mu    sync.Mutex
	paths []string
}

func (q *fakeQuitter) QuitForUpdate(p string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paths = append(q.paths, p)
}

func (q *fakeQuitter) calls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.paths)
}

var now = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)

type rig struct {
	db      *sql.DB
	store   *updatecontrol.Store
	checker *fakeChecker
	quit    *fakeQuitter
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &rig{db: db, store: updatecontrol.New(db), checker: &fakeChecker{safe: true}, quit: &fakeQuitter{}}
}

// run starts a bridge and returns a function that stops it and waits.
func (r *rig) run(t *testing.T) (stop func()) {
	t.Helper()
	return r.runAt(t, now)
}

// runAt is run with the bridge's clock fixed at clock.
func (r *rig) runAt(t *testing.T, clock time.Time) (stop func()) {
	t.Helper()
	b := mcpbridge.New(mcpbridge.Config{Store: r.store, Checker: r.checker, Quitter: r.quit, Version: "v0.1.0", Now: func() time.Time { return clock }, Tick: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx)
	}()
	r.awaitStatus(t) // the start-up drop of stale requests has happened by then
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop
}

// awaitStatus waits for the bridge's first publication and returns it.
func (r *rig) awaitStatus(t *testing.T) updatecontrol.Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, ok, err := updatecontrol.ReadStatus(t.Context(), r.db)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("no status published")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *rig) enqueue(t *testing.T, kind string, at time.Time) int64 {
	t.Helper()
	id, err := updatecontrol.InsertRequest(t.Context(), r.db, at, kind, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (r *rig) await(t *testing.T, id int64) updatecontrol.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, err := updatecontrol.GetRequest(t.Context(), r.db, id)
		if err != nil {
			t.Fatal(err)
		}
		if req.Done() {
			return req
		}
		if time.Now().After(deadline) {
			t.Fatalf("request %d not finished: %+v", id, req)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A request a previous run of the application left pending is dropped at
// start-up: nothing a caller has long given up on may run later.
func TestBridgeDropsRequestsLeftByAPreviousRun(t *testing.T) {
	r := newRig(t)
	r.checker.result = updater.Result{Ready: true, Version: "v0.2.0", InstallerPath: "/x"}
	id := r.enqueue(t, updatecontrol.KindApply, now)
	r.run(t)
	if req := r.await(t, id); req.Outcome != updatecontrol.OutcomeExpired {
		t.Errorf("outcome = %q, want expired", req.Outcome)
	}
	if r.quit.calls() != 0 || r.checker.checkCount() != 0 {
		t.Errorf("quits %d, checks %d, want none", r.quit.calls(), r.checker.checkCount())
	}
}

// A request that waited longer than MaxQueueAge is dropped unexecuted.
func TestBridgeDropsOverdueRequests(t *testing.T) {
	r := newRig(t)
	r.run(t)
	id := r.enqueue(t, updatecontrol.KindCheck, now.Add(-time.Hour))
	if req := r.await(t, id); req.Outcome != updatecontrol.OutcomeExpired || r.checker.checkCount() != 0 {
		t.Errorf("outcome = %q, checks %d, want expired and no check", req.Outcome, r.checker.checkCount())
	}
}

// update_apply installs only what this bridge's own check verified: a Ready
// flag in the Checker's status alone (another check's) is not enough.
func TestBridgeApplyNeedsItsOwnVerifiedInstaller(t *testing.T) {
	r := newRig(t)
	r.checker.status = updater.Status{Available: true, Version: "v0.2.0", Ready: true}
	r.run(t)
	req := r.await(t, r.enqueue(t, updatecontrol.KindApply, now))
	if req.Outcome != updatecontrol.OutcomeDeferred || req.Reason != updatecontrol.ReasonNotReady || r.quit.calls() != 0 {
		t.Errorf("apply = %+v, quits %d, want deferred/not_ready", req, r.quit.calls())
	}
}

// SafeGate is evaluated again at apply time: a gate that closed since the check defers it.
func TestBridgeApplyReevaluatesSafeGate(t *testing.T) {
	r := newRig(t)
	r.checker.result = updater.Result{Ready: true, Version: "v0.2.0", InstallerPath: "/x"}
	r.checker.status = updater.Status{Available: true, Version: "v0.2.0", Ready: true}
	r.checker.safe = false
	r.run(t)
	if req := r.await(t, r.enqueue(t, updatecontrol.KindCheck, now)); req.Outcome != updatecontrol.OutcomeChecked {
		t.Fatalf("check = %+v", req)
	}
	req := r.await(t, r.enqueue(t, updatecontrol.KindApply, now))
	if req.Outcome != updatecontrol.OutcomeDeferred || req.Reason != "kill_switch" || r.quit.calls() != 0 {
		t.Errorf("apply = %+v, quits %d, want deferred/kill_switch", req, r.quit.calls())
	}
}

// The published status carries kinds only, and is withdrawn when the bridge stops.
func TestBridgePublishesKindsAndRetiresOnStop(t *testing.T) {
	r := newRig(t)
	r.checker.status = updater.Status{
		CheckedAt: now, Available: true, Version: "v0.2.0", Blocked: true, BlockedKind: updater.BlockRecentOrder,
		LastError: "GET https://api.github.com/secret/path: dial tcp", ErrorKind: updater.ErrorNetwork,
	}
	stop := r.run(t)
	got := r.awaitStatus(t)
	if got.CurrentVersion != "v0.1.0" || got.LatestVersion != "v0.2.0" || !got.Blocked || got.BlockedKind != "recent_order" || got.ErrorKind != "network" || got.CheckedAt == nil {
		t.Errorf("published = %+v", got)
	}
	stop()
	if _, ok, err := updatecontrol.ReadStatus(t.Context(), r.db); err != nil || ok {
		t.Errorf("status after stop: present=%v err=%v, want withdrawn", ok, err)
	}
}

func jst(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.FixedZone("JST", 9*60*60))
}

// The 立会時間帯 (a trading day's JST 9:00-15:30, lunch break included) defers
// update_apply with market_hours even though SafeGate passes; outside it, or on
// a day the exchange is closed, SafeGate alone decides. SafeGate's own kind
// wins over market_hours when both hold the install back.
func TestBridgeApplyDefersDuringMarketHours(t *testing.T) {
	const holiday = 23 // 2026-11-23 Mon, 勤労感謝の日
	cases := []struct {
		name       string
		clock      time.Time
		safe       bool
		wantReason string // "" = accepted
	}{
		{"morning session", jst(2026, 11, 25, 9, 0), true, "market_hours"},
		{"lunch break", jst(2026, 11, 25, 12, 0), true, "market_hours"},
		{"last minute", jst(2026, 11, 25, 15, 29), true, "market_hours"},
		{"before open", jst(2026, 11, 25, 8, 59), true, ""},
		{"at close", jst(2026, 11, 25, 15, 30), true, ""},
		{"evening", jst(2026, 11, 25, 20, 0), true, ""},
		{"weekday holiday at noon", jst(2026, 11, holiday, 10, 0), true, ""},
		{"saturday at noon", jst(2026, 11, 28, 10, 0), true, ""},
		{"holiday, SafeGate blocks", jst(2026, 11, holiday, 10, 0), false, "kill_switch"},
		{"market hours, SafeGate blocks", jst(2026, 11, 25, 10, 0), false, "kill_switch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.checker.result = updater.Result{Ready: true, Version: "v0.2.0", InstallerPath: "/x"}
			r.checker.status = updater.Status{Available: true, Version: "v0.2.0", Ready: true}
			r.checker.safe = c.safe // the fake's check does not consult it
			r.runAt(t, c.clock)
			if req := r.await(t, r.enqueue(t, updatecontrol.KindCheck, c.clock)); req.Outcome != updatecontrol.OutcomeChecked {
				t.Fatalf("check = %+v", req)
			}
			req := r.await(t, r.enqueue(t, updatecontrol.KindApply, c.clock))
			if c.wantReason == "" {
				if req.Outcome != updatecontrol.OutcomeAccepted || r.quit.calls() != 1 {
					t.Errorf("apply = %+v, quits %d, want accepted and one quit", req, r.quit.calls())
				}
				return
			}
			if req.Outcome != updatecontrol.OutcomeDeferred || req.Reason != c.wantReason || r.quit.calls() != 0 {
				t.Errorf("apply = %+v, quits %d, want deferred/%s and no quit", req, r.quit.calls(), c.wantReason)
			}
		})
	}
}

// During the 立会時間帯 the published status says a release is held back for
// market_hours (what update_status shows); SafeGate's kind keeps precedence,
// and outside the hours nothing is added.
func TestBridgePublishesMarketHoursAsBlockedKind(t *testing.T) {
	cases := []struct {
		name        string
		clock       time.Time
		status      updater.Status
		wantBlocked bool
		wantKind    string
	}{
		{"available during hours", jst(2026, 11, 25, 10, 0), updater.Status{Available: true, Version: "v0.2.0", Ready: true}, true, "market_hours"},
		{"nothing available", jst(2026, 11, 25, 10, 0), updater.Status{}, false, ""},
		{"SafeGate kind wins", jst(2026, 11, 25, 10, 0), updater.Status{Available: true, Version: "v0.2.0", Blocked: true, BlockedKind: updater.BlockRecentOrder}, true, "recent_order"},
		{"after close", jst(2026, 11, 25, 16, 0), updater.Status{Available: true, Version: "v0.2.0", Ready: true}, false, ""},
		{"holiday", jst(2026, 11, 23, 10, 0), updater.Status{Available: true, Version: "v0.2.0", Ready: true}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.checker.status = c.status
			r.runAt(t, c.clock)
			if got := r.awaitStatus(t); got.Blocked != c.wantBlocked || got.BlockedKind != c.wantKind {
				t.Errorf("published blocked=%v kind=%q, want %v / %q", got.Blocked, got.BlockedKind, c.wantBlocked, c.wantKind)
			}
		})
	}
}

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
	b := mcpbridge.New(mcpbridge.Config{Store: r.store, Checker: r.checker, Quitter: r.quit, Version: "v0.1.0", Now: func() time.Time { return now }, Tick: 10 * time.Millisecond})
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

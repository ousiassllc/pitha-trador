package updatetests

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"net/http"
	"os"
	"slices"
	"testing"
)

type applyData struct {
	Result     string `json:"result"`
	Reason     string `json:"reason"`
	ReasonText string `json:"reason_text"`
	RequestID  int64  `json:"request_id"`
}

func apply(t *testing.T, st *stack) applyData {
	t.Helper()
	var d applyData
	st.call(t, "update_apply", map[string]any{}).data(t, "update_apply", &d)
	return d
}

// checkNow runs a check and returns the tool's data.
func checkNow(t *testing.T, st *stack) updateData {
	t.Helper()
	var d updateData
	st.call(t, "update_check_now", map[string]any{}).data(t, "update_check_now", &d)
	return d
}

// Writes off: update_apply is refused with write_disabled; nothing is
// installed and no audit row is written (the verified installer stays pending).
func TestUpdateApplyRefusedWhenWriteDisabled(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	if d := checkNow(t, st); !d.Ready {
		t.Fatalf("setup: %+v, want a ready installer", d)
	}
	st.call(t, "settings_write_disable", map[string]any{})

	r := st.call(t, "update_apply", map[string]any{})
	if r.status != http.StatusForbidden || r.errorCode(t) != "write_disabled" {
		t.Fatalf("update_apply = %d %s, want 403 write_disabled", r.status, r.body)
	}
	if len(st.quit.calls()) != 0 || len(st.applyRows(t)) != 0 {
		t.Errorf("quit %v, audit rows %v: a refused apply must install nothing and record nothing", st.quit.calls(), st.applyRows(t))
	}

	st.call(t, "settings_write_enable", map[string]any{}) // turning writes back on restores it
	if d := apply(t, st); d.Result != "accepted" {
		t.Errorf("update_apply after enabling = %+v, want accepted", d)
	}
}

// SafeGate holding the release back (evaluated again right before the install)
// gives deferred with the kind; the application is not quit.
func TestUpdateApplyDeferredWhenSafeGateBlocks(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	if d := checkNow(t, st); !d.Ready || d.Blocked {
		t.Fatalf("setup: %+v, want ready and not blocked", d)
	}
	st.gate.openPositions.Store(2) // a position opened after the download

	d := apply(t, st)
	if d.Result != "deferred" || d.Reason != "open_positions" || d.ReasonText != "ポジションを保有しているため" {
		t.Errorf("update_apply = %+v, want deferred by open_positions", d)
	}
	if len(st.quit.calls()) != 0 {
		t.Errorf("quit called %v while SafeGate blocks", st.quit.calls())
	}
	if rows := st.applyRows(t); len(rows) != 1 || rows[0].Outcome != "deferred" || rows[0].Reason != "open_positions" {
		t.Errorf("audit rows = %v, want one deferred/open_positions", rows)
	}
}

// Without a verified installer (no check yet) update_apply defers with not_ready.
func TestUpdateApplyDeferredWhenNothingIsReady(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	d := apply(t, st)
	if d.Result != "deferred" || d.Reason != "not_ready" || len(st.quit.calls()) != 0 || st.gh.hits.Load() != 0 {
		t.Errorf("update_apply = %+v, quit %v, GitHub lookups %d, want deferred/not_ready and no download", d, st.quit.calls(), st.gh.hits.Load())
	}
}

// A release whose signature does not verify is never installed: the check
// fails with ErrorVerification, and update_apply has nothing to install.
func TestUpdateApplyNeverInstallsFailedVerification(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	st.gh.badSig = true
	d := checkNow(t, st)
	if d.Check != "failed" || d.ErrorKind != "verification" || d.Ready {
		t.Fatalf("update_check_now = %+v, want a failed check with error_kind verification", d)
	}
	if a := apply(t, st); a.Result != "deferred" || a.Reason != "not_ready" {
		t.Errorf("update_apply = %+v, want deferred/not_ready", a)
	}
	if len(st.quit.calls()) != 0 {
		t.Errorf("quit called %v for a release that failed verification", st.quit.calls())
	}
	if got := status(t, st); got.ErrorKind != "verification" || got.Ready {
		t.Errorf("update_status = %+v, want the verification error kept", got)
	}
}

// A passed update_apply installs through the existing QuitForUpdate with the
// verified installer, and leaves its audit row: actor=mcp, the caller's host.
func TestUpdateApplyAcceptedWritesAuditRowWithActorMCP(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	if d := checkNow(t, st); !d.Ready {
		t.Fatalf("setup: %+v, want a ready installer", d)
	}
	d := apply(t, st)
	if d.Result != "accepted" || d.RequestID == 0 {
		t.Fatalf("update_apply = %+v, want accepted", d)
	}
	paths := st.quit.calls()
	if len(paths) != 1 {
		t.Fatalf("QuitForUpdate calls = %v, want exactly one", paths)
	}
	if body, err := os.ReadFile(paths[0]); err != nil || string(body) != installerBody {
		t.Errorf("installer %s: %q (%v), want the downloaded and verified one", paths[0], body, err)
	}
	want := applyRow{Actor: "mcp", Client: "127.0.0.1", State: "done", Outcome: "accepted"}
	if rows := st.applyRows(t); !slices.Equal(rows, []applyRow{want}) {
		t.Errorf("audit rows = %v, want %v", rows, want)
	}
	if got := status(t, st); got.LastApply == nil || got.LastApply.Actor != "mcp" || got.LastApply.Result != "accepted" {
		t.Errorf("update_status.last_apply = %+v, want the accepted mcp request", got.LastApply)
	}
	if again := apply(t, st); again.Result != "deferred" || again.Reason != "not_ready" || len(st.quit.calls()) != 1 {
		t.Errorf("a second update_apply = %+v (quits %d), want deferred/not_ready: one verified installer, one install", again, len(st.quit.calls()))
	}
}

// An application that never answers cannot install late: the request is
// withdrawn when the call gives up, so a restart finds nothing to run.
func TestUpdateApplyWithdrawnWhenApplicationDoesNotAnswer(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", false)
	// The application published a status, then stopped reading requests.
	testkit.MustExec(t, st.db, `INSERT INTO update_control_status (id, heartbeat_at, current_version, dev_build, no_release, available, blocked, ready)
		VALUES (1, ?, 'v0.1.0', 0, 0, 1, 0, 1)`, testkit.FixedNow().Format(time.RFC3339))

	d := apply(t, st)
	if d.Result != "deferred" || d.Reason != "timeout" {
		t.Fatalf("update_apply = %+v, want deferred/timeout", d)
	}
	if rows := st.applyRows(t); len(rows) != 1 || rows[0].State != "done" || rows[0].Outcome != "expired" {
		t.Errorf("audit rows = %v, want one expired request that can no longer run", rows)
	}
	st.runBridge(t)
	checkNow(t, st) // answered only after the restarted application served its queue
	if len(st.quit.calls()) != 0 {
		t.Errorf("quit called %v for a withdrawn request", st.quit.calls())
	}
}

package updatetests

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// updateData is the shared shape of update_status and update_check_now.
type updateData struct {
	Unavailable       bool   `json:"unavailable"`
	UnavailableReason string `json:"unavailable_reason"`
	CurrentVersion    string `json:"current_version"`
	LatestVersion     string `json:"latest_version"`
	CheckedAt         string `json:"checked_at"`
	Available         bool   `json:"available"`
	Blocked           bool   `json:"blocked"`
	Ready             bool   `json:"ready"`
	DevBuild          bool   `json:"dev_build"`
	NoRelease         bool   `json:"no_release"`
	BlockedKind       string `json:"blocked_kind"`
	BlockedReason     string `json:"blocked_reason"`
	ErrorKind         string `json:"error_kind"`
	ErrorReason       string `json:"error_reason"`
	Check             string `json:"check"`
	LastApply         *struct {
		Actor, Client, Result, Reason string
	} `json:"last_apply"`
}

func status(t *testing.T, st *stack) updateData {
	t.Helper()
	var d updateData
	st.call(t, "update_status", map[string]any{}).data(t, "update_status", &d)
	return d
}

// update_status reads what the application published: this build's version, the
// latest release seen, available / blocked / ready, the SafeGate kind with its
// Settings wording, and the time of the check.
func TestUpdateStatusReadsPublishedState(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	st.gate.openPositions.Store(1) // SafeGate holds the release back
	var check updateData
	st.call(t, "update_check_now", map[string]any{}).data(t, "update_check_now", &check)

	got := status(t, st)
	if got.Unavailable || got.CurrentVersion != "v0.1.0" || got.LatestVersion != "v0.2.0" || !got.Available || !got.Blocked || got.Ready {
		t.Errorf("status = %+v, want available and blocked at v0.1.0 -> v0.2.0, not ready", got)
	}
	if got.BlockedKind != "open_positions" || got.BlockedReason != "ポジションを保有しているため" {
		t.Errorf("blocked = %q / %q, want open_positions with the Settings wording", got.BlockedKind, got.BlockedReason)
	}
	if _, err := time.Parse(time.RFC3339, got.CheckedAt); err != nil || got.ErrorKind != "" {
		t.Errorf("checked_at %q (%v), error_kind %q, want a time and no error", got.CheckedAt, err, got.ErrorKind)
	}
}

// The output names no path, URL, key or raw error text, even after a failed check.
func TestUpdateStatusShowsNoRawErrorPathURLOrKey(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	st.gh.badSig = true // the check fails with a verification error whose text names paths and URLs
	st.call(t, "update_check_now", map[string]any{})

	raw := string(st.call(t, "update_status", map[string]any{}).body)
	for _, leak := range []string{st.gh.URL, "127.0.0.1", "checksums", "installer.exe", "/tmp", "signature verification", string(publicKey)} {
		if strings.Contains(raw, leak) {
			t.Errorf("update_status leaks %q: %s", leak, raw)
		}
	}
	if d := status(t, st); d.ErrorKind != "verification" || !strings.Contains(d.ErrorReason, "検証に失敗") {
		t.Errorf("error = %q / %q, want verification with its wording", d.ErrorKind, d.ErrorReason)
	}
}

// Without an application publishing a status (cmd/server, or the application
// is not running) the tools answer "no updater" instead of failing.
func TestUpdateToolsReportUnavailableWithoutUpdater(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", false)
	if d := status(t, st); !d.Unavailable || d.UnavailableReason == "" || d.Available {
		t.Errorf("update_status = %+v, want unavailable", d)
	}
	var check updateData
	st.call(t, "update_check_now", map[string]any{}).data(t, "update_check_now", &check)
	var n int
	if err := st.db.QueryRowContext(t.Context(), `SELECT count(*) FROM update_control_requests`).Scan(&n); err != nil || n != 0 || !check.Unavailable {
		t.Errorf("update_check_now = %+v, requests %d (%v), want unavailable and nothing queued", check, n, err)
	}
	var apply struct{ Result, Reason string }
	st.call(t, "update_apply", map[string]any{}).data(t, "update_apply", &apply)
	if apply.Result != "deferred" || apply.Reason != "unavailable" || len(st.quit.calls()) != 0 {
		t.Errorf("update_apply = %+v, want deferred/unavailable and no quit", apply)
	}
}

// A status row the application left behind when it died is not trusted.
func TestUpdateStatusIgnoresStaleHeartbeat(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", false)
	stale := testkit.FixedNow().Add(-2 * time.Hour).Format(time.RFC3339)
	testkit.MustExec(t, st.db, `INSERT INTO update_control_status (id, heartbeat_at, current_version, dev_build, no_release, available, blocked, ready)
		VALUES (1, ?, 'v0.1.0', 0, 0, 1, 0, 0)`, stale)
	if d := status(t, st); !d.Unavailable {
		t.Errorf("update_status = %+v, want unavailable for a stale heartbeat", d)
	}
}

// update_check_now runs Checker.CheckForUpdate (one GitHub lookup, the same
// checkMu as the scheduler's tick), and neither quits nor installs.
func TestUpdateCheckNowUsesCheckForUpdatePath(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	st.gh.hold = 150 * time.Millisecond
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = st.checker.CheckForUpdate(t.Context()) }) // the scheduler's periodic tick
	waitFor(t, func() bool { return st.gh.inFlight.Load() > 0 })    // the tick holds checkMu inside its lookup

	var d updateData
	st.call(t, "update_check_now", map[string]any{}).data(t, "update_check_now", &d)
	wg.Wait()

	if d.Check != "completed" || !d.Available || !d.Ready || d.LatestVersion != "v0.2.0" {
		t.Errorf("update_check_now = %+v, want a completed check with a ready v0.2.0", d)
	}
	if got := st.checker.Status(); !got.Ready || got.Version != "v0.2.0" {
		t.Errorf("Checker.Status = %+v, want the check recorded there", got)
	}
	if n := st.gh.hits.Load(); n != 2 {
		t.Errorf("GitHub lookups = %d, want 2 (the tick and the tool)", n)
	}
	if m := st.gh.maxInFight.Load(); m != 1 {
		t.Errorf("concurrent lookups = %d, want 1 (checkMu serializes them)", m)
	}
	if len(st.quit.calls()) != 0 {
		t.Errorf("quit called %v: update_check_now must never install", st.quit.calls())
	}
}

// A dev build skips the check as before: no GitHub call, dev_build=true.
func TestUpdateCheckNowDevBuildSkipsCheck(t *testing.T) {
	st := newStack(t, "dev", "v0.2.0", true)
	var d updateData
	st.call(t, "update_check_now", map[string]any{}).data(t, "update_check_now", &d)
	if !d.DevBuild || d.Check != "completed" || d.Available || st.gh.hits.Load() != 0 {
		t.Errorf("update_check_now = %+v, GitHub lookups %d, want dev_build and no lookup", d, st.gh.hits.Load())
	}
}

// No argument of any tool can name a version, URL or asset or skip SafeGate or
// the signature check: the schemas are closed and empty, and extras are refused.
func TestUpdateToolsTakeNoArguments(t *testing.T) {
	st := newStack(t, "v0.1.0", "v0.2.0", true)
	for _, tool := range []string{"update_status", "update_check_now", "update_apply"} {
		for _, args := range []map[string]any{
			{"version": "v0.0.1"}, {"url": "https://example.com/x.exe"}, {"asset": "other.exe"}, {"force": true},
			{"skip_safegate": true}, {"skip_signature": true}, {"downgrade": true},
		} {
			r := st.call(t, tool, args)
			if r.status != http.StatusUnprocessableEntity || r.errorCode(t) != "invalid_argument" {
				t.Errorf("%s %v: status %d %s, want 422 invalid_argument", tool, args, r.status, r.body)
			}
		}
	}
	if len(st.quit.calls()) != 0 || len(st.applyRows(t)) != 0 || st.gh.hits.Load() != 0 {
		t.Error("a refused call must do nothing")
	}
}

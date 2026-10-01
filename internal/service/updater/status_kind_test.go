package updater_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

// The Settings panel names the gate that holds a newer release back (issue
// #241), so Status must carry which gate it was, not only that one did.
func TestChecker_Status_BlockedKindNamesTheFailingGate(t *testing.T) {
	setVersion(t, "v0.1.0")
	killed := fakeSystemStateReader{state: domain.SystemStateKilled}
	running := fakeSystemStateReader{state: domain.SystemStateRunning}

	cases := []struct {
		name string
		gate updater.SafeGate
		want updater.BlockKind
	}{
		{"open position", updater.SafeGate{Positions: fakePositionCounter{count: 1}, State: running, Orders: fakeOrderLister{}}, updater.BlockOpenPositions},
		{"kill switch", updater.SafeGate{Positions: fakePositionCounter{}, State: killed, Orders: fakeOrderLister{}}, updater.BlockKillSwitch},
		{"recent order", updater.SafeGate{
			Positions: fakePositionCounter{}, State: running,
			Orders: fakeOrderLister{orders: []domain.PaperOrder{{SubmittedAt: time.Now().Add(-time.Minute)}}},
		}, updater.BlockRecentOrder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newChecker(newGitHubMock(t, "v0.2.0", ""), tc.gate)
			if _, err := checker.CheckForUpdate(context.Background()); err != nil {
				t.Fatalf("CheckForUpdate: %v", err)
			}
			if status := checker.Status(); !status.Blocked || status.BlockedKind != tc.want {
				t.Fatalf("Status = %+v, want Blocked with BlockedKind %q", status, tc.want)
			}
		})
	}
}

// Each failure class the Settings panel distinguishes (issue #241) must
// come out of a real failing check as its own ErrorKind.
func TestChecker_Status_ErrorKindClassifiesFailures(t *testing.T) {
	setVersion(t, "v0.1.0")
	badChecksums := fmt.Sprintf("%064d  %s\n", 0, installerName)

	status := func(t *testing.T, server *httptest.Server) updater.Status {
		t.Helper()
		checker := newChecker(server, allowGate())
		if _, err := checker.CheckForUpdate(context.Background()); err == nil {
			t.Fatal("CheckForUpdate err = nil, want a failure")
		}
		return checker.Status()
	}
	apiStatus := func(code int, header http.Header) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(code)
		}))
		t.Cleanup(server.Close)
		return server
	}

	unreachable := newGitHubMock(t, "v0.2.0", "")
	unreachable.Close()

	cases := []struct {
		name   string
		server *httptest.Server
		want   updater.ErrorKind
	}{
		{"unreachable API", unreachable, updater.ErrorNetwork},
		{"429 rate limit", apiStatus(http.StatusTooManyRequests, nil), updater.ErrorRateLimit},
		{"403 with no quota left", apiStatus(http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}}), updater.ErrorRateLimit},
		{"plain 403", apiStatus(http.StatusForbidden, nil), updater.ErrorAccess},
		{"401 bad token", apiStatus(http.StatusUnauthorized, nil), updater.ErrorAccess},
		{"404 private repo or unpublished", apiStatus(http.StatusNotFound, nil), updater.ErrorAccess},
		{"API 500", apiStatus(http.StatusInternalServerError, nil), updater.ErrorNetwork},
		{"200 with undecodable body", apiStatus(http.StatusOK, nil), updater.ErrorRelease},
		{"non-semver tag", newGitHubMock(t, "latest", ""), updater.ErrorRelease},
		{"checksum mismatch", newGitHubMock(t, "v0.2.0", badChecksums), updater.ErrorVerification},
		{"checksum entry missing", newGitHubMock(t, "v0.2.0", "\n"), updater.ErrorVerification},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := status(t, tc.server); got.ErrorKind != tc.want || got.LastError == "" {
				t.Fatalf("Status = %+v, want ErrorKind %q with LastError set", got, tc.want)
			}
		})
	}
}

// A later successful check clears the earlier failure's kind.
func TestChecker_Status_SuccessClearsErrorKind(t *testing.T) {
	setVersion(t, "v0.1.0")
	server := newGitHubMock(t, "v0.1.0", "")
	checker := newChecker(server, allowGate())
	server.Close()
	if _, err := checker.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("CheckForUpdate err = nil, want a network error")
	}
	if got := checker.Status().ErrorKind; got != updater.ErrorNetwork {
		t.Fatalf("ErrorKind = %q, want network", got)
	}

	healthy := newGitHubMock(t, "v0.1.0", "")
	checker = newChecker(healthy, allowGate())
	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if got := checker.Status(); got.ErrorKind != "" || got.LastError != "" {
		t.Fatalf("Status = %+v, want no error after a successful check", got)
	}
}

// Issue #259: the scheduler's retry loop must be able to tell failures that
// only a new release/configuration can fix from ones that clear by
// themselves, through the optional Permanent() marker on the error chain.
func TestChecker_CheckForUpdate_ErrorPermanence(t *testing.T) {
	setVersion(t, "v0.1.0")
	badChecksums := fmt.Sprintf("%064d  %s\n", 0, installerName)
	status := func(code int) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		t.Cleanup(server.Close)
		return server
	}
	unreachable := newGitHubMock(t, "v0.2.0", "")
	unreachable.Close()

	cases := []struct {
		name          string
		server        *httptest.Server
		wantPermanent bool
	}{
		{"unreachable API", unreachable, false},
		{"rate limit", status(http.StatusTooManyRequests), false},
		{"API 500", status(http.StatusInternalServerError), false},
		{"access refused", status(http.StatusNotFound), true},
		{"non-semver tag", newGitHubMock(t, "latest", ""), true},
		{"checksum mismatch", newGitHubMock(t, "v0.2.0", badChecksums), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newChecker(tc.server, allowGate()).CheckForUpdate(context.Background())
			if err == nil {
				t.Fatal("CheckForUpdate err = nil, want a failure")
			}
			var p interface{ Permanent() bool }
			if got := errors.As(err, &p) && p.Permanent(); got != tc.wantPermanent {
				t.Fatalf("permanent = %v, want %v (err: %v)", got, tc.wantPermanent, err)
			}
		})
	}
}

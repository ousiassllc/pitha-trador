package updater_test

import (
	"context"
	"encoding/json"
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

	// Issue #259: the scheduler's retry loop skips failures that only a new
	// release or configuration can fix, via the error's Permanent() marker.
	permanent := map[updater.ErrorKind]bool{
		updater.ErrorAccess: true, updater.ErrorVerification: true, updater.ErrorRelease: true,
	}
	status := func(t *testing.T, server *httptest.Server, want updater.ErrorKind) updater.Status {
		t.Helper()
		checker := newChecker(server, allowGate())
		_, err := checker.CheckForUpdate(context.Background())
		if err == nil {
			t.Fatal("CheckForUpdate err = nil, want a failure")
		}
		var p interface{ Permanent() bool }
		if got := errors.As(err, &p) && p.Permanent(); got != permanent[want] {
			t.Errorf("Permanent() = %v, want %v for %q (err: %v)", got, permanent[want], want, err)
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

	// assetStatus serves a healthy release whose asset downloads all answer
	// with code (issue #280).
	assetStatus := func(code int, header http.Header) *httptest.Server {
		var serverURL string
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
				{Name: installerName, BrowserDownloadURL: serverURL + "/download/installer.exe"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
			}})
		})
		mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(code)
		})
		server := httptest.NewServer(mux)
		serverURL = server.URL
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
		{"403 with Retry-After", apiStatus(http.StatusForbidden, http.Header{"Retry-After": {"60"}}), updater.ErrorRateLimit},
		{"plain 403", apiStatus(http.StatusForbidden, nil), updater.ErrorAccess},
		{"401 unauthorized", apiStatus(http.StatusUnauthorized, nil), updater.ErrorAccess},
		{"API 500", apiStatus(http.StatusInternalServerError, nil), updater.ErrorNetwork},
		{"200 with undecodable body", apiStatus(http.StatusOK, nil), updater.ErrorRelease},
		{"non-semver tag", newGitHubMock(t, "latest", ""), updater.ErrorRelease},
		{"asset 429 rate limit", assetStatus(http.StatusTooManyRequests, nil), updater.ErrorRateLimit},
		{"asset 403 with Retry-After", assetStatus(http.StatusForbidden, http.Header{"Retry-After": {"60"}}), updater.ErrorRateLimit},
		{"asset plain 403", assetStatus(http.StatusForbidden, nil), updater.ErrorAccess},
		{"asset 401 unauthorized", assetStatus(http.StatusUnauthorized, nil), updater.ErrorAccess},
		{"asset 404 removed", assetStatus(http.StatusNotFound, nil), updater.ErrorAccess},
		{"asset 500", assetStatus(http.StatusInternalServerError, nil), updater.ErrorNetwork},
		{"checksum mismatch", newGitHubMock(t, "v0.2.0", badChecksums), updater.ErrorVerification},
		{"checksum entry missing", newGitHubMock(t, "v0.2.0", "\n"), updater.ErrorVerification},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := status(t, tc.server, tc.want); got.ErrorKind != tc.want || got.LastError == "" {
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

// Issue #296: a 404 on the latest-release lookup (nothing published yet, or
// the repository is not visible) is not a failure - no error for the
// scheduler to log at ERROR and retry with backoff - but Status.NoRelease for
// the Settings panel. A later published release clears it.
func TestChecker_Status_NoReleaseIsNotAnError(t *testing.T) {
	setVersion(t, "v0.1.0")
	notFound := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(notFound.Close)
	checker := newChecker(notFound, allowGate())

	result, err := checker.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate err = %v, want nil for a 404 release lookup", err)
	}
	if result.Ready {
		t.Fatal("result.Ready = true, want false")
	}
	got := checker.Status()
	if !got.NoRelease || got.CheckedAt.IsZero() || got.LastError != "" || got.ErrorKind != "" || got.Available {
		t.Fatalf("Status = %+v, want NoRelease with CheckedAt set and no error", got)
	}

	published := newGitHubMock(t, "v0.1.0", "")
	checker = newChecker(published, allowGate())
	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if checker.Status().NoRelease {
		t.Fatal("NoRelease still set after a release became available")
	}
}

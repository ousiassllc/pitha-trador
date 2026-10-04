// Package checkflow_test holds the Checker download-hardening and Status
// classification tests. They only use updater's exported API and live in
// their own directory to keep internal/service/updater under the linterly
// line budget (#398). The helpers below are this package's own (sibling test
// packages do not import each other).
package checkflow_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// setVersion overrides internal/version.Version for the duration of the
// test (t.Cleanup restores it), since Checker.CheckForUpdate compares the
// latest release's tag_name against this build-time-embedded global.
func setVersion(t *testing.T, v string) {
	t.Helper()
	original := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = original })
}

type fakePositionCounter struct{ count int }

func (f fakePositionCounter) OpenPositionCount(context.Context) (int, error) { return f.count, nil }

type fakeSystemStateReader struct{ state domain.SystemState }

func (f fakeSystemStateReader) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return f.state, nil, nil
}

type fakeOrderLister struct{ orders []domain.PaperOrder }

func (f fakeOrderLister) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return f.orders, nil
}

// allowGate is a SafeGate every call to SafeToUpdate reports safe for.
func allowGate() updater.SafeGate {
	return updater.SafeGate{
		Positions: fakePositionCounter{count: 0},
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}
}

const installerBody = "fake-installer-bytes-for-issue-65-tests"

func installerChecksum(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(installerBody))
	return hex.EncodeToString(sum[:])
}

// newGitHubMock serves `/repos/{owner}/{repo}/releases/latest` plus the
// installer/checksums asset download URLs it references.
func newGitHubMock(t *testing.T, tagName string, checksumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	var serverURL string
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		release := updater.Release{
			TagName: tagName,
			Assets: []updater.Asset{
				{Name: installerName, BrowserDownloadURL: serverURL + "/download/installer.exe"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/download/installer.exe", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(installerBody))
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumsBody))
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func newChecker(server *httptest.Server, gate updater.SafeGate) *updater.Checker {
	return updater.NewChecker(updater.Config{
		Owner:      "ousiassllc",
		Repo:       "pitha-trador",
		Gate:       gate,
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		// newGitHubMock serves assets under /download/ on the same server.
		DownloadURLPrefix: server.URL + "/download/",
	})
}

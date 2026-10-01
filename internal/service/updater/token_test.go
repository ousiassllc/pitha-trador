package updater_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

const privateRepoToken = "ghp_private_repo_token"

// newPrivateRepoMock behaves like GitHub for a private repository (issue
// #265): every unauthenticated request - the release lookup as well as the
// browser_download_url downloads - is answered 404, and only requests
// carrying the token succeed. Assets are then only downloadable through
// their API `url` with `Accept: application/octet-stream`.
func newPrivateRepoMock(t *testing.T) *httptest.Server {
	t.Helper()
	authorized := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer "+privateRepoToken }
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
			{Name: installerName, URL: serverURL + "/repos/ousiassllc/pitha-trador/releases/assets/1", BrowserDownloadURL: serverURL + "/download/installer.exe"},
			{Name: "checksums.txt", URL: serverURL + "/repos/ousiassllc/pitha-trador/releases/assets/2", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
		}})
	})
	asset := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authorized(r) || r.Header.Get("Accept") != "application/octet-stream" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/assets/1", asset(installerBody))
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/assets/2", asset(fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)))
	mux.HandleFunc("/download/", http.NotFound) // browser_download_url is not usable on a private repo
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func privateRepoChecker(server *httptest.Server, token string) *updater.Checker {
	return updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "pitha-trador", Gate: allowGate(),
		HTTPClient: server.Client(), BaseURL: server.URL, DownloadURLPrefix: server.URL + "/download/",
		Token: token,
	})
}

// Regresses issue #265: an unauthenticated check against a private
// repository got a 404 that surfaced as the opaque "release info could not
// be fetched or is invalid" failure. With a token the whole pipeline
// (lookup, API asset download, checksum) must succeed.
func TestChecker_CheckForUpdate_PrivateRepoWithToken(t *testing.T) {
	setVersion(t, "v0.1.0")
	server := newPrivateRepoMock(t)

	result, err := privateRepoChecker(server, privateRepoToken).CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !result.Ready || result.Version != "v0.2.0" {
		t.Fatalf("result = %+v, want Ready v0.2.0", result)
	}
}

// Without a token the private repository's 404 must be reported as an
// access problem, not as invalid release content (issue #265).
func TestChecker_CheckForUpdate_PrivateRepoWithoutTokenIsAccessError(t *testing.T) {
	setVersion(t, "v0.1.0")
	checker := privateRepoChecker(newPrivateRepoMock(t), "")

	if _, err := checker.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("CheckForUpdate err = nil, want a failure")
	}
	if got := checker.Status().ErrorKind; got != updater.ErrorAccess {
		t.Fatalf("ErrorKind = %q, want %q", got, updater.ErrorAccess)
	}
}

// The token must never be sent to an asset URL outside the repository's
// API asset path.
func TestChecker_CheckForUpdate_TokenNotSentToForeignAssetURL(t *testing.T) {
	setVersion(t, "v0.1.0")
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign host was contacted with Authorization=%q", r.Header.Get("Authorization"))
	}))
	t.Cleanup(evil.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
			{Name: installerName, URL: evil.URL + "/a/1"},
			{Name: "checksums.txt", URL: evil.URL + "/a/2"},
		}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	checker := privateRepoChecker(server, privateRepoToken)
	if _, err := checker.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("CheckForUpdate err = nil, want a rejection")
	}
	if got := checker.Status().ErrorKind; got != updater.ErrorVerification {
		t.Fatalf("ErrorKind = %q, want %q", got, updater.ErrorVerification)
	}
}

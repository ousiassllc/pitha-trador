package updater_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// A private repository's 404 must be reported as an access problem, not as
// invalid release content (issue #265) - and, when the token itself is
// rejected, as its own failure: the operator must fix the token, not set one.
func TestChecker_CheckForUpdate_PrivateRepoRefusalKinds(t *testing.T) {
	setVersion(t, "v0.1.0")
	for token, want := range map[string]updater.ErrorKind{"": updater.ErrorAccess, "ghp_wrong_token": updater.ErrorAuth} {
		checker := privateRepoChecker(newPrivateRepoMock(t), token)
		if _, err := checker.CheckForUpdate(context.Background()); err == nil {
			t.Fatalf("token %q: CheckForUpdate err = nil, want a failure", token)
		}
		if got := checker.Status().ErrorKind; got != want {
			t.Errorf("token %q: ErrorKind = %q, want %q", token, got, want)
		}
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

func TestChecker_CheckForUpdate_TokenIsTrimmed(t *testing.T) {
	setVersion(t, "v0.1.0")
	checker := privateRepoChecker(newPrivateRepoMock(t), " \t"+privateRepoToken+"\n")
	if result, err := checker.CheckForUpdate(context.Background()); err != nil || !result.Ready {
		t.Fatalf("CheckForUpdate = %+v, %v; want Ready", result, err)
	}
}

// GitHub answers the API asset request with a 302 to a signed URL on another
// host (objects.githubusercontent.com). The token must not follow it there:
// net/http drops Authorization on a cross-host redirect, and this pins that
// the download still works that way.
func TestChecker_CheckForUpdate_TokenNotForwardedToCrossHostRedirect(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("CDN received Authorization=%q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/checksums" {
			_, _ = w.Write([]byte(checksums))
			return
		}
		_, _ = w.Write([]byte(installerBody))
	}))
	t.Cleanup(cdn.Close)
	// Same loopback IP, but a different hostname: a different "host" for
	// net/http's redirect header policy.
	cdnHost := strings.Replace(cdn.URL, "127.0.0.1", "localhost", 1)

	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
			{Name: installerName, URL: serverURL + "/repos/ousiassllc/pitha-trador/releases/assets/1"},
			{Name: "checksums.txt", URL: serverURL + "/repos/ousiassllc/pitha-trador/releases/assets/2"},
		}})
	})
	redirect := func(path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+privateRepoToken {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, cdnHost+path, http.StatusFound)
		}
	}
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/assets/1", redirect("/installer"))
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/assets/2", redirect("/checksums"))
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	result, err := privateRepoChecker(server, privateRepoToken).CheckForUpdate(context.Background())
	if err != nil || !result.Ready {
		t.Fatalf("CheckForUpdate = %+v, %v; want Ready", result, err)
	}
}

package updater_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// testPrivateKey/testPublicKey are a throwaway ed25519 pair derived from a
// fixed, public test seed; they sign nothing outside these tests.
var (
	testPrivateKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	testPublicKey  = testPrivateKey.Public().(ed25519.PublicKey)
)

// signChecksums is checksums.txt.sig's body for content under testPrivateKey.
func signChecksums(content string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(testPrivateKey, []byte(content)))
}

// setVersion overrides internal/version.Version for the duration of the
// test (t.Cleanup restores it), since Checker.CheckForUpdate compares the
// latest release's tag_name against this build-time-embedded global. It
// also embeds testPublicKey as internal/version.ReleasePublicKey, as a
// release build does: a Checker without a key refuses every update (issue
// #891).
func setVersion(t *testing.T, v string) {
	t.Helper()
	original, originalKey := version.Version, version.ReleasePublicKey
	version.Version = v
	version.ReleasePublicKey = base64.StdEncoding.EncodeToString(testPublicKey)
	t.Cleanup(func() { version.Version, version.ReleasePublicKey = original, originalKey })
}

// allowGate is a SafeGate every call to SafeToUpdate reports safe for -
// tests that only care about the semver/download/checksum pipeline use
// it, so they do not also have to fake risk/execution dependencies.
func allowGate() updater.SafeGate {
	return updater.SafeGate{
		Positions: fakePositionCounter{count: 0},
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}
}

const installerBody = "fake-installer-bytes-for-issue-65-tests"

const installerName = "pitha-trador-windows-amd64-installer.exe"

func installerChecksum(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(installerBody))
	return hex.EncodeToString(sum[:])
}

// newGitHubMock serves `/repos/{owner}/{repo}/releases/latest` plus the
// installer/checksums asset download URLs it references, standing in for
// GitHub's real API + asset CDN (issue #65's acceptance criteria: "GitHub
// API・ダウンロードをモックする").
func newGitHubMock(t *testing.T, tagName string, checksumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	var serverURL string
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		release := updater.Release{
			TagName: tagName,
			Assets: []updater.Asset{
				{Name: "pitha-trador-windows-amd64-installer.exe", BrowserDownloadURL: serverURL + "/download/installer.exe"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
				{Name: "checksums.txt.sig", BrowserDownloadURL: serverURL + "/download/checksums.txt.sig"},
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
	// Release authenticity is mandatory (issue #891): the mock signs
	// checksumsBody with testPrivateKey; setVersion embeds the matching key.
	mux.HandleFunc("/download/checksums.txt.sig", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(signChecksums(checksumsBody)))
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

// newChecker relies on setVersion's embedded testPublicKey, the key
// newGitHubMock signs with.
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

func TestChecker_CheckForUpdate_NoNewerVersion(t *testing.T) {
	setVersion(t, "v0.1.1")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.1.1", checksums) // same as current version

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.Ready {
		t.Fatalf("Ready = true, want false (latest release == current version)")
	}
}

func TestChecker_CheckForUpdate_OlderRemoteVersion(t *testing.T) {
	setVersion(t, "v0.2.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.1.9", checksums) // latest release is older than current

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.Ready {
		t.Fatalf("Ready = true, want false (latest release older than current)")
	}
}

func TestChecker_CheckForUpdate_DevBuildSkipsCheckEntirely(t *testing.T) {
	setVersion(t, "dev")
	// No mock server at all: if CheckForUpdate performed any HTTP call
	// against a nonexistent server it would return an error, not a clean
	// zero Result.
	checker := updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "pitha-trador",
		Gate:    allowGate(),
		BaseURL: "http://127.0.0.1:1", // deliberately unreachable
	})

	result, err := checker.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v, want nil (dev build must skip before any network call)", err)
	}
	if result.Ready {
		t.Fatalf("Ready = true, want false (dev build)")
	}
}

func TestChecker_CheckForUpdate_NewerVersionDownloadsAndVerifies(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.2.0", checksums)

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !result.Ready {
		t.Fatal("Ready = false, want true (newer release, checksum matches, gate clear)")
	}
	if result.Version != "v0.2.0" {
		t.Fatalf("Version = %q, want %q", result.Version, "v0.2.0")
	}
	defer func() { _ = os.RemoveAll(result.InstallerPath) }()

	got, err := os.ReadFile(result.InstallerPath)
	if err != nil {
		t.Fatalf("read downloaded installer: %v", err)
	}
	if string(got) != installerBody {
		t.Fatalf("downloaded installer content = %q, want %q", got, installerBody)
	}
}

func TestChecker_CheckForUpdate_ChecksumMismatchAborts(t *testing.T) {
	setVersion(t, "v0.1.0")
	// Deliberately wrong hash.
	checksums := "0000000000000000000000000000000000000000000000000000000000000000  pitha-trador-windows-amd64-installer.exe\n"
	server := newGitHubMock(t, "v0.2.0", checksums)

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err == nil {
		t.Fatal("CheckForUpdate: err = nil, want a checksum-mismatch error")
	}
	if result.Ready {
		t.Fatalf("Ready = true, want false (checksum mismatch must abort)")
	}
}

func TestChecker_CheckForUpdate_UnsafeGateBlocksDownload(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.2.0", checksums)

	unsafeGate := updater.SafeGate{
		Positions: fakePositionCounter{count: 1}, // an open position blocks the gate
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}

	result, err := newChecker(server, unsafeGate).CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.Ready {
		t.Fatalf("Ready = true, want false (open position must block the safety gate)")
	}
}

func TestChecker_CheckForUpdate_MissingInstallerAssetErrors(t *testing.T) {
	setVersion(t, "v0.1.0")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		release := updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{{Name: "checksums.txt", BrowserDownloadURL: "http://unused"}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err == nil {
		t.Fatal("CheckForUpdate: err = nil, want an error (no installer asset)")
	}
	if result.Ready {
		t.Fatal("Ready = true, want false (no installer asset)")
	}
}

// A GitHub API outage/rate-limit must not be silently treated as
// "up to date": SafeGate's caller-visible ready=false only ever means
// "nothing to do", never "we could not tell".
func TestChecker_CheckForUpdate_GitHubAPIErrorPropagates(t *testing.T) {
	setVersion(t, "v0.1.0")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	_, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err == nil {
		t.Fatal("CheckForUpdate: err = nil, want an error (GitHub API 503)")
	}
}

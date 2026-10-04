package checkflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

const installerName = "pitha-trador-windows-amd64-installer.exe"

// newAssetMock serves a release whose installer/checksums Asset entries are
// built by the caller (issue #126 hardening tests need to control Size and
// BrowserDownloadURL), with the given raw installer body.
func newAssetMock(t *testing.T, installerBody string, assets func(serverURL string) []updater.Asset) *httptest.Server {
	t.Helper()
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: assets(serverURL)})
	})
	mux.HandleFunc("/download/installer.exe", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(installerBody))
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", installerChecksum(t), installerName)
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func standardAssets(installerSize int64) func(string) []updater.Asset {
	return func(u string) []updater.Asset {
		return []updater.Asset{
			{Name: installerName, BrowserDownloadURL: u + "/download/installer.exe", Size: installerSize},
			{Name: "checksums.txt", BrowserDownloadURL: u + "/download/checksums.txt"},
		}
	}
}

func TestChecker_CheckForUpdate_RejectsDownloadURLOutsideReleasePrefix(t *testing.T) {
	setVersion(t, "v0.1.0")
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("evil host was contacted")
	}))
	t.Cleanup(evil.Close)

	cases := map[string]func(string) string{
		"other host":     func(string) string { return evil.URL + "/download/installer.exe" },
		"outside prefix": func(u string) string { return u + "/other/installer.exe" },
		"path traversal": func(u string) string { return u + "/download/../other/installer.exe" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			server := newAssetMock(t, installerBody, func(u string) []updater.Asset {
				a := standardAssets(0)(u)
				a[0].BrowserDownloadURL = mutate(u)
				return a
			})
			result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
			if err == nil || !strings.Contains(err.Error(), "outside") {
				t.Fatalf("err = %v, want an 'outside' download-url error", err)
			}
			if result.Ready {
				t.Fatal("Ready = true, want false")
			}
		})
	}
}

func TestChecker_CheckForUpdate_RejectsBodyLargerThanReportedSize(t *testing.T) {
	setVersion(t, "v0.1.0")
	body := strings.Repeat("x", 1024)
	server := newAssetMock(t, body, standardAssets(16)) // GitHub says 16 bytes

	result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
	if result.Ready {
		t.Fatal("Ready = true, want false")
	}
}

func TestChecker_CheckForUpdate_RejectsAssetSizeAboveCap(t *testing.T) {
	setVersion(t, "v0.1.0")
	server := newAssetMock(t, installerBody, standardAssets(1<<40))

	_, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("err = %v, want a size-cap error", err)
	}
}

// A stalled GitHub API must fail within MetadataTimeout instead of holding
// checkMu (and every manual "今すぐ確認" behind it) forever.
func TestChecker_CheckForUpdate_MetadataTimeout(t *testing.T) {
	setVersion(t, "v0.1.0")
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	checker := updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "pitha-trador", Gate: allowGate(),
		HTTPClient: server.Client(), BaseURL: server.URL,
		MetadataTimeout: 50 * time.Millisecond,
	})
	start := time.Now()
	_, err := checker.CheckForUpdate(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v, want prompt timeout", elapsed)
	}
}

func TestChecker_CheckForUpdate_DownloadTimeout(t *testing.T) {
	setVersion(t, "v0.1.0")
	done := make(chan struct{})
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: standardAssets(0)(serverURL)})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })

	checker := updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "pitha-trador", Gate: allowGate(),
		HTTPClient: server.Client(), BaseURL: server.URL,
		DownloadURLPrefix: server.URL + "/download/",
		DownloadTimeout:   50 * time.Millisecond,
	})
	_, err := checker.CheckForUpdate(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

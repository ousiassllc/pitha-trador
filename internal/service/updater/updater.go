package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/ousiassllc/pitha-trador/internal/version"
)

// defaultBaseURL is the production GitHub API base URL latestRelease
// queries. Config.BaseURL overrides it (tests point it at an
// httptest.Server instead).
const defaultBaseURL = "https://api.github.com"

// installerAssetSuffix/checksumsAssetName match .github/workflows/ci.yml's
// `wails build -nsis` output naming (issue #64:
// `sha256sum build/bin/*-installer.exe > build/bin/checksums.txt`).
const (
	installerAssetSuffix = "-installer.exe"
	checksumsAssetName   = "checksums.txt"
)

// Default deadlines and size caps (issue #126). http.DefaultClient has no
// overall timeout, so a stalled connection would otherwise block
// CheckForUpdate - and every later manual check queued behind checkMu -
// forever. The deadlines are applied per request through the context so
// they hold even for a caller-supplied Config.HTTPClient. The caps bound
// what an abnormal or malicious response can write to memory/disk.
const (
	defaultMetadataTimeout = 30 * time.Second
	defaultDownloadTimeout = 10 * time.Minute

	maxReleaseJSONBytes = 1 << 20   // 1 MiB: GET /releases/latest response
	maxChecksumsBytes   = 1 << 20   // 1 MiB: checksums.txt
	maxInstallerBytes   = 512 << 20 // 512 MiB: NSIS installer
)

// Release is the subset of GitHub's
// `GET /repos/{owner}/{repo}/releases/latest` response Checker needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is one GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Size is the asset's byte size as reported by GitHub. When positive it
	// must not exceed the download cap and the downloaded body must match it.
	Size int64 `json:"size,omitempty"`
}

// Result is CheckForUpdate's outcome (package doc comment).
type Result struct {
	// Ready is true only once a newer release's installer has been
	// downloaded and its checksum verified; the caller may now trigger
	// the actual install. False with a nil error covers every
	// "nothing to do yet" case: already up to date, or a newer release
	// exists but SafeGate.SafeToUpdate currently rejects it (retried on
	// the next scheduler tick).
	Ready bool
	// Version is the newer release's tag_name, set whenever Ready.
	Version string
	// InstallerPath is the verified installer's local temp-file path, set
	// whenever Ready. The caller is responsible for removing it once
	// installed.
	InstallerPath string
}

// Config configures a new Checker.
type Config struct {
	// Owner/Repo identify the GitHub repository CheckForUpdate polls
	// ("ousiassllc"/"pitha-trador" in production).
	Owner, Repo string
	// Gate is issue #65's mandatory safety gate; SafeToUpdate must pass
	// before any download is attempted.
	Gate SafeGate

	// HTTPClient defaults to a plain http.Client; request deadlines come
	// from MetadataTimeout/DownloadTimeout, not from the client.
	HTTPClient *http.Client
	// BaseURL overrides defaultBaseURL (tests only).
	BaseURL string
	// DownloadURLPrefix overrides the only URL prefix asset downloads are
	// allowed from, "https://github.com/<Owner>/<Repo>/releases/download/"
	// by default (tests only).
	DownloadURLPrefix string
	// MetadataTimeout bounds the release lookup request (default 30s).
	MetadataTimeout time.Duration
	// DownloadTimeout bounds the whole asset download phase (default 10m).
	DownloadTimeout time.Duration
}

// Checker implements issue #65's periodic GitHub Releases update check
// (package doc comment).
type Checker struct {
	owner, repo string
	gate        SafeGate
	httpClient  *http.Client
	baseURL     string

	downloadURLPrefix string
	metadataTimeout   time.Duration
	downloadTimeout   time.Duration

	// checkMu serializes CheckForUpdate: the scheduler's periodic tick
	// and the Settings screen's manual "今すぐ確認" (issue #76) may
	// otherwise download the installer and trigger the quit twice.
	checkMu sync.Mutex

	// statusMu guards status (Status).
	statusMu sync.Mutex
	status   Status
}

// NewChecker returns a Checker configured by cfg.
func NewChecker(cfg Config) *Checker {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	downloadURLPrefix := cfg.DownloadURLPrefix
	if downloadURLPrefix == "" {
		downloadURLPrefix = fmt.Sprintf("https://github.com/%s/%s/releases/download/", cfg.Owner, cfg.Repo)
	}
	metadataTimeout := cfg.MetadataTimeout
	if metadataTimeout <= 0 {
		metadataTimeout = defaultMetadataTimeout
	}
	downloadTimeout := cfg.DownloadTimeout
	if downloadTimeout <= 0 {
		downloadTimeout = defaultDownloadTimeout
	}
	return &Checker{
		owner:             cfg.Owner,
		repo:              cfg.Repo,
		gate:              cfg.Gate,
		httpClient:        httpClient,
		baseURL:           baseURL,
		downloadURLPrefix: downloadURLPrefix,
		metadataTimeout:   metadataTimeout,
		downloadTimeout:   downloadTimeout,
	}
}

// CheckForUpdate runs the full pipeline the package doc comment
// describes: skip entirely on a non-release ("dev") build, fetch the
// latest release, semver-compare it against internal/version.Version,
// check SafeGate.SafeToUpdate, then download+verify the installer. Every
// outcome is also recorded for Status (issue #76).
func (c *Checker) CheckForUpdate(ctx context.Context) (Result, error) {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()

	result, err := c.check(ctx)
	if err != nil {
		c.setStatusError(err)
	}
	return result, err
}

func (c *Checker) check(ctx context.Context) (Result, error) {
	if !semver.IsValid(version.Version) {
		slog.Debug("updater: skipping check on a non-release (dev) build", "version", version.Version)
		c.setStatus(Status{CheckedAt: time.Now(), DevBuild: true})
		return Result{}, nil
	}

	release, err := c.latestRelease(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("updater: fetch latest release: %w", err)
	}
	if !semver.IsValid(release.TagName) {
		return Result{}, fmt.Errorf("updater: latest release tag_name %q is not valid semver", release.TagName)
	}
	if semver.Compare(release.TagName, version.Version) <= 0 {
		c.setStatus(Status{CheckedAt: time.Now()}) // already up to date
		return Result{}, nil
	}

	status := Status{CheckedAt: time.Now(), Available: true, Version: release.TagName}
	safe, reason := c.gate.SafeToUpdate(ctx)
	if !safe {
		slog.Info("updater: newer release available but not safe to update yet",
			"current", version.Version, "latest", release.TagName, "reason", reason)
		status.Blocked = true
		c.setStatus(status)
		return Result{}, nil
	}
	c.setStatus(status)

	installerAsset, checksumAsset, err := selectAssets(release.Assets)
	if err != nil {
		return Result{}, fmt.Errorf("updater: %s: %w", release.TagName, err)
	}

	installerPath, err := c.downloadAndVerify(ctx, installerAsset, checksumAsset)
	if err != nil {
		return Result{}, fmt.Errorf("updater: download/verify %s: %w", release.TagName, err)
	}

	slog.Info("updater: new release downloaded and verified", "version", release.TagName, "installer", installerPath)
	status.Ready = true
	c.setStatus(status)
	return Result{Ready: true, Version: release.TagName, InstallerPath: installerPath}, nil
}

// latestRelease calls `GET /repos/{owner}/{repo}/releases/latest`.
func (c *Checker) latestRelease(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, c.owner, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseJSONBytes)).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("decode response: %w", err)
	}
	return release, nil
}

// selectAssets finds the NSIS installer and its checksums.txt among
// assets (issue #64's naming: `*-installer.exe` + `checksums.txt`).
func selectAssets(assets []Asset) (installer, checksums Asset, err error) {
	var foundInstaller, foundChecksums bool
	for _, a := range assets {
		switch {
		case strings.HasSuffix(a.Name, installerAssetSuffix):
			installer, foundInstaller = a, true
		case a.Name == checksumsAssetName:
			checksums, foundChecksums = a, true
		}
	}
	if !foundInstaller {
		return Asset{}, Asset{}, fmt.Errorf("no %s asset found", installerAssetSuffix)
	}
	if !foundChecksums {
		return Asset{}, Asset{}, fmt.Errorf("no %s asset found", checksumsAssetName)
	}
	return installer, checksums, nil
}

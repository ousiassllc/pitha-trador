package updater

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
	// whenever Ready. It lives in a per-download temp directory that is
	// not removed once the installer has run (a running installer cannot
	// delete itself on Windows): the next startup removes it via
	// CleanupStaleDownloads.
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
	// PublicKey is the raw ed25519 key checksums.txt.sig must verify under
	// (issue #376). It defaults to internal/version.ReleasePublicKey, the
	// key embedded at build time; when neither is set, signature
	// verification is skipped.
	PublicKey ed25519.PublicKey
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

	// publicKey verifies checksums.txt.sig (issue #376); nil disables the
	// check. publicKeyErr records a configured but malformed key, which must
	// fail every download closed rather than silently disable the check.
	publicKey    ed25519.PublicKey
	publicKeyErr error

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
	publicKey, publicKeyErr := resolvePublicKey(cfg.PublicKey)
	return &Checker{
		owner:             cfg.Owner,
		repo:              cfg.Repo,
		gate:              cfg.Gate,
		httpClient:        httpClient,
		baseURL:           baseURL,
		downloadURLPrefix: downloadURLPrefix,
		metadataTimeout:   metadataTimeout,
		downloadTimeout:   downloadTimeout,
		publicKey:         publicKey,
		publicKeyErr:      publicKeyErr,
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
	if errors.Is(err, errNoRelease) {
		// Not a failure: nothing is published yet (or the repository is not
		// visible), so there is nothing to update to. Surfacing it as an
		// error would log at ERROR and retry with backoff every tick
		// (issue #296); Status.NoRelease lets the Settings panel say so.
		slog.Info("updater: no release found (none published, or repository not accessible)", "owner", c.owner, "repo", c.repo)
		c.setStatus(Status{CheckedAt: time.Now(), NoRelease: true})
		return Result{}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("updater: fetch latest release: %w", err)
	}
	if !semver.IsValid(release.TagName) {
		return Result{}, kindErrorf(ErrorRelease, "updater: latest release tag_name %q is not valid semver", release.TagName)
	}
	if semver.Compare(release.TagName, version.Version) <= 0 {
		c.setStatus(Status{CheckedAt: time.Now()}) // already up to date
		return Result{}, nil
	}

	status := Status{CheckedAt: time.Now(), Available: true, Version: release.TagName}
	if c.holdBack(ctx, &status, release.TagName, "before download") {
		return Result{}, nil
	}

	assets, err := selectAssets(release.Assets)
	if err != nil {
		return Result{}, withKind(ErrorRelease, fmt.Errorf("updater: %s: %w", release.TagName, err))
	}

	installerPath, err := c.downloadAndVerify(ctx, assets)
	if err != nil {
		return Result{}, fmt.Errorf("updater: download/verify %s: %w", release.TagName, err)
	}

	// The download can take minutes (up to defaultDownloadTimeout), during
	// which a position may have opened, the Kill Switch fired or an order
	// been submitted; the caller quits and installs right after Ready, so
	// re-evaluate the gate now (issue #535). The unused installer is
	// discarded; the next check downloads it again.
	if c.holdBack(ctx, &status, release.TagName, "after download") {
		_ = os.RemoveAll(filepath.Dir(installerPath))
		return Result{}, nil
	}

	slog.Info("updater: new release downloaded and verified", "version", release.TagName, "installer", installerPath)
	status.Ready = true
	c.setStatus(status)
	return Result{Ready: true, Version: release.TagName, InstallerPath: installerPath}, nil
}

// holdBack evaluates SafeGate. When it does not pass it records status as
// Blocked (so the scheduler retries soon, SchedulerAdapter.UpdatePending)
// and reports true; otherwise it records status unchanged and reports
// false. phase only labels the log line.
func (c *Checker) holdBack(ctx context.Context, status *Status, tag, phase string) bool {
	safe, reason := c.gate.SafeToUpdate(ctx)
	if !safe {
		slog.Info("updater: newer release available but not safe to update yet",
			"current", version.Version, "latest", tag, "phase", phase, "reason", reason.Detail)
		status.Ready = false
		status.Blocked = true
		status.BlockedKind = reason.Kind
	}
	c.setStatus(*status)
	return !safe
}

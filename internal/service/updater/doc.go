// Package updater implements issue #65's automatic GitHub Releases update
// check and safe, unattended self-update pipeline for cmd/desktop's Wails
// build only - cmd/server is headless and has no installer concept, so it
// never wires this package in at all (internal/bootstrap/services.go's
// BuildServices only does so when its autoUpdate parameter is non-nil).
//
// Checker.CheckForUpdate runs the full read-only-until-verified pipeline
// once at startup and on every internal/service/scheduler @every-6h tick
// (scheduler.WithUpdateChecker, via SchedulerAdapter below; the scheduler
// also retries a failed or safety-gate-held check with backoff, issue
// #240): fetch the
// latest GitHub release, compare its tag_name against
// internal/version.Version (golang.org/x/mod/semver - no hand-rolled
// semver parsing), and - only once SafeGate.SafeToUpdate's three-part
// safety gate (no open positions, Kill Switch inactive, no order
// submitted within the last MinIdleAfterOrder) passes - download the
// installer asset, verify it against the published checksums.txt (issue
// #64), and report the verified local path back in its Result.
//
// Fetch hardening (issue #126): every request carries a context deadline
// (Config.MetadataTimeout for the release lookup, Config.DownloadTimeout
// for the whole download phase), so a stalled connection cannot hold
// Checker's checkMu forever. Asset downloads are size-capped
// (maxInstallerBytes/maxChecksumsBytes, tightened to Asset.Size when GitHub
// reports one) and Asset.BrowserDownloadURL must live under
// https://github.com/<owner>/<repo>/releases/download/.
//
// Release authenticity (issue #376): checksums.txt comes from the same
// release as the installer, so SHA256 alone only detects corruption, not a
// replaced release (compromised token or release job). ci.yml therefore
// also publishes checksums.txt.sig, a base64 detached ed25519 signature of
// checksums.txt made with a GitHub Actions secret, and the matching public
// key is embedded in the binary (internal/version.ReleasePublicKey,
// overridable by Config.PublicKey). When a key is configured,
// downloadAndVerify verifies the signature right after fetching
// checksums.txt - before the installer is downloaded or run - and a
// missing, malformed or mismatching signature (or a malformed key) is a
// Permanent ErrorVerification that removes every downloaded file. Known
// limitation: a build without a public key (no signing key provisioned)
// skips the signature check, logs a warning and relies on SHA256 alone;
// Authenticode code signing is not implemented.
//
// A 404 on the latest-release lookup (no release published, or repository
// not accessible) is not a failure: the check succeeds with Status.NoRelease
// set, is logged at Info and is not retried with backoff (issue #296).
// A refused lookup (401/403) or asset download (401/403/404) is ErrorAccess, distinct
// from ErrorRelease
// (fetched but unusable content); a rate-limited one (429, or 403 with
// Retry-After) is ErrorRateLimit.
// ErrorAccess/ErrorVerification/ErrorRelease report Permanent() so the
// scheduler's backoff does not retry them (issue #259).
//
// Checker itself never runs the installer or quits the process: that is
// SchedulerAdapter's Quitter's job (cmd/desktop/app.go's QuitForUpdate -
// Wails' runtime.Quit, then its shutdown spawns
// BuildSilentInstallCommand's *exec.Cmd), since both need the Wails
// runtime context this package cannot depend on (see below).
//
// This package MUST depend only on internal/domain and internal/repository
// (internal/service/doc.go) plus its own I/O (net/http to GitHub) - it
// does not import internal/service/risk or internal/service/execution
// directly. SafeGate's PositionCounter/SystemStateReader/OrderLister
// interfaces mirror those two packages' existing method signatures
// instead (the same pattern internal/service/scheduler/periodic.go's own
// HeartbeatChecker interface already established for the identical
// reason), and internal/bootstrap wires the very same
// *repoportfolio.Provider/*risk.Engine/*execution.Engine
// instances it already builds in as those interfaces - so position/order
// aggregation is never re-implemented here.
package updater

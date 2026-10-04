// Package version holds this build's semver version string, embedded at
// build time by .github/workflows/ci.yml's `wails build -ldflags "-X
// .../internal/version.Version=${GITHUB_REF_NAME}"` step - tag pushes
// only (GITHUB_REF_NAME is the pushed tag name, e.g. "v0.1.1", which is
// already valid semver per that workflow's tagging convention). Branch/PR
// builds never set this flag, so Version stays "dev" there.
//
// internal/service/updater (issue #65) is the only reader: it treats
// "dev" (not valid semver, golang.org/x/mod/semver.IsValid) as "no
// release baseline to compare against" and skips its update check
// entirely, rather than risk deciding a dev build is "outdated" against
// every tagged release.
package version

// Version is this build's semver tag, or "dev" for a branch/PR build.
var Version = "dev"

// ReleasePublicKey is the base64 (standard encoding) raw ed25519 public key
// that internal/service/updater verifies checksums.txt.sig against before
// trusting a release's installer (issue #376). ci.yml embeds it with
// `-ldflags "-X .../internal/version.ReleasePublicKey=<repository variable
// RELEASE_SIGNING_PUBLIC_KEY>"`; it stays empty - signature verification
// off - for builds made without a signing key (see docs/environment/setup.md).
var ReleasePublicKey = ""

// GitHubOwner and GitHubRepo identify the GitHub repository whose releases
// internal/service/updater checks for a newer build.
const (
	GitHubOwner = "ousiassllc"
	GitHubRepo  = "pitha-trador"
)

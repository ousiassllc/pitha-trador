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

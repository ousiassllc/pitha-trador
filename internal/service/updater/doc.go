// Package updater implements issue #65's automatic GitHub Releases update
// check and safe, unattended self-update pipeline for cmd/desktop's Wails
// build only - cmd/server is headless and has no installer concept, so it
// never wires this package in at all (internal/bootstrap/services.go's
// BuildServices only does so when its autoUpdate parameter is non-nil).
//
// Checker.CheckForUpdate runs the full read-only-until-verified pipeline
// on every internal/service/scheduler @every-6h tick
// (scheduler.WithUpdateChecker, via SchedulerAdapter below): fetch the
// latest GitHub release, compare its tag_name against
// internal/version.Version (golang.org/x/mod/semver - no hand-rolled
// semver parsing), and - only once SafeGate.SafeToUpdate's three-part
// safety gate (no open positions, Kill Switch inactive, no order
// submitted within the last MinIdleAfterOrder) passes - download the
// installer asset, verify it against the published checksums.txt (issue
// #64), and report the verified local path back in its Result.
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
// *risk.RepositoryPortfolioProvider/*risk.Engine/*execution.Engine
// instances it already builds in as those interfaces - so position/order
// aggregation is never re-implemented here.
package updater

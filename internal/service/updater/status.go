package updater

import "time"

// Status is a snapshot of the most recent Checker.CheckForUpdate outcome
// (issue #76), for the UI to tell the operator a newer release exists and
// whether installing it is still waiting on SafeGate. The zero value means
// "no check has completed yet" (CheckedAt.IsZero).
type Status struct {
	// CheckedAt is when the most recent check finished, successfully or
	// not; zero before the first one.
	CheckedAt time.Time
	// DevBuild is true when the last check was skipped because this is a
	// non-release ("dev") build (internal/version.Version is not semver).
	DevBuild bool
	// Available is true while a release newer than the running version
	// exists; Version is that release's tag_name.
	Available bool
	Version   string
	// Blocked is true when Available but SafeGate.SafeToUpdate rejected
	// the installation on the last check (retried on the next one), so
	// the app keeps running until the gate passes.
	Blocked bool
	// Ready is true once the newer installer was downloaded and verified
	// (the caller is about to quit and run it).
	Ready bool
	// LastError is the last check's failure message, empty when it
	// succeeded. A failed check keeps the previous Available/Version/
	// Blocked, since the failure says nothing about whether the newer
	// release still exists.
	LastError string
}

// Status returns the most recent check's outcome.
func (c *Checker) Status() Status {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.status
}

func (c *Checker) setStatus(s Status) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.status = s
}

// setStatusError records a failed check on top of the previous status.
func (c *Checker) setStatusError(err error) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.status.CheckedAt = time.Now()
	c.status.LastError = err.Error()
}

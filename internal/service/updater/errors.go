package updater

import (
	"errors"
	"fmt"
)

// ErrorKind classifies why a check failed, so the Settings panel can tell
// the operator what went wrong without exposing the raw error text (issue
// #241). The zero value means "no error".
type ErrorKind string

const (
	// ErrorNetwork: the GitHub API or an asset download could not be
	// reached, timed out, the transfer broke off, or GitHub answered with
	// a server-side error status.
	ErrorNetwork ErrorKind = "network"
	// ErrorRateLimit: GitHub rejected the release lookup for rate limiting.
	ErrorRateLimit ErrorKind = "rate_limit"
	// ErrorAccess: GitHub refused an unauthenticated release lookup - 401/403
	// or 404 (the repository is private, or no release is published). Issue #265:
	// this used to be reported as ErrorRelease, indistinguishable from
	// invalid release content.
	ErrorAccess ErrorKind = "access"
	// ErrorAuth: like ErrorAccess, but a token is configured, so GitHub
	// rejected the token itself (expired, insufficient scope, or no access
	// to this repository).
	ErrorAuth ErrorKind = "auth"
	// ErrorVerification: a downloaded asset was rejected - checksum
	// mismatch or missing entry, size limit, or a URL outside the
	// repository's release-download path.
	ErrorVerification ErrorKind = "verification"
	// ErrorRelease: the release was fetched but its content is unusable -
	// undecodable JSON, non-semver tag, or missing installer / checksums
	// asset.
	ErrorRelease ErrorKind = "release"
	// ErrorOther: anything else (e.g. local temp-file failures).
	ErrorOther ErrorKind = "other"
)

// kindedError tags err with its ErrorKind; Unwrap keeps errors.Is/As
// working through it.
type kindedError struct {
	kind ErrorKind
	err  error
}

func (e *kindedError) Error() string { return e.err.Error() }
func (e *kindedError) Unwrap() error { return e.err }

// Permanent implements the optional interface scheduler/updatecheck.Runner
// consults (issue #259): a broken release, a rejected asset or a refused
// lookup stays broken until the release or the configuration changes, so
// retrying it every backoff step (and re-downloading the whole installer
// for a checksum mismatch) is wasted work; the next @every-6h tick covers
// the case that it was fixed meanwhile. Network, rate-limit and other
// errors may clear by themselves and are retried with backoff.
func (e *kindedError) Permanent() bool {
	switch e.kind {
	case ErrorAccess, ErrorAuth, ErrorVerification, ErrorRelease:
		return true
	default:
		return false
	}
}

func withKind(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &kindedError{kind: kind, err: err}
}

func kindErrorf(kind ErrorKind, format string, args ...any) error {
	return &kindedError{kind: kind, err: fmt.Errorf(format, args...)}
}

// errorKind returns the outermost ErrorKind err carries, ErrorOther when
// it carries none.
func errorKind(err error) ErrorKind {
	var ke *kindedError
	if errors.As(err, &ke) {
		return ke.kind
	}
	return ErrorOther
}

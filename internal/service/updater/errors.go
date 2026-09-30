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
	// reached, timed out, or the transfer broke off.
	ErrorNetwork ErrorKind = "network"
	// ErrorRateLimit: GitHub rejected the release lookup for rate limiting.
	ErrorRateLimit ErrorKind = "rate_limit"
	// ErrorVerification: a downloaded asset was rejected - checksum
	// mismatch or missing entry, size limit, or a URL outside the
	// repository's release-download path.
	ErrorVerification ErrorKind = "verification"
	// ErrorRelease: the release itself is unusable - unexpected API
	// status, undecodable JSON, non-semver tag, or missing installer /
	// checksums asset.
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

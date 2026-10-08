package tachibana

import (
	"errors"
	"log/slog"
	"net/url"
)

// Target selects which virtual URL a request goes to. All three share one
// serial queue (REQUEST I/F: one question, one answer).
type Target int

const (
	// TargetRequest is the 業務機能 URL (sUrlRequest); also logout.
	TargetRequest Target = iota
	// TargetMaster is the マスタ機能 URL (sUrlMaster).
	TargetMaster
	// TargetPrice is the 時価情報機能 URL (sUrlPrice).
	TargetPrice
)

const redacted = "[redacted]"

// virtualURLs are the five virtual URLs of one login. They are the bearer
// credential of the session, so the type redacts itself in every formatting
// and logging path (fmt verbs, %#v, slog): only code inside this package
// can read the fields.
type virtualURLs struct {
	request, master, price, event, eventWebSocket string
}

func (virtualURLs) String() string { return redacted }

func (virtualURLs) GoString() string { return redacted }

func (virtualURLs) LogValue() slog.Value { return slog.StringValue(redacted) }

func (u virtualURLs) of(t Target) string {
	switch t {
	case TargetMaster:
		return u.master
	case TargetPrice:
		return u.price
	}
	return u.request
}

// transportError returns err without the request URL: net/http wraps
// failures in a *url.Error whose message carries the full URL, which for a
// virtual URL would leak the session credential into logs and the UI. The
// underlying cause stays reachable through errors.Is/As.
func transportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &requestError{cause: urlErr.Err}
	}
	return err
}

type requestError struct{ cause error }

func (e *requestError) Error() string { return "tachibana: request failed: " + e.cause.Error() }

func (e *requestError) Unwrap() error { return e.cause }

package gmo

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxRetryAfter caps how long a server's Retry-After can make the Client
// wait before one retry. A larger value (or an absurd HTTP-date) must not
// stall a caller for hours; the Client waits MaxRetryAfter instead.
const MaxRetryAfter = time.Minute

// parseRetryAfter reads an HTTP Retry-After value (RFC 9110 §10.2.3): either
// delta-seconds or an HTTP-date, the latter measured against now. It returns
// the wait capped at MaxRetryAfter, or 0 when the header is absent, malformed,
// negative or already in the past (the caller then uses its own backoff).
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.ParseUint(v, 10, 63); err == nil {
		// Compare before multiplying so a huge value cannot overflow.
		if secs >= uint64(MaxRetryAfter/time.Second) {
			return MaxRetryAfter
		}
		d = time.Duration(secs) * time.Second
	} else if errors.Is(err, strconv.ErrRange) {
		return MaxRetryAfter
	} else if t, perr := http.ParseTime(v); perr == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), MaxRetryAfter)
}

// retryWait is the sleep before the next attempt: the exponential backoff,
// lengthened (never shortened) to the server's Retry-After when that is
// larger. The failure need not be an APIError; then it is the backoff.
func retryWait(backoff time.Duration, err error) time.Duration {
	var ae *APIError
	if errors.As(err, &ae) {
		return max(backoff, ae.RetryAfter)
	}
	return backoff
}

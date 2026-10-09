package broker

import "errors"

var (
	// ErrNoSession is returned by calls that need an authenticated session
	// when the adapter holds none yet (kabu: no token issued yet).
	ErrNoSession = errors.New("broker: no session established yet")
	// ErrRateLimited is returned after the broker's request limit kept
	// rejecting a call through the adapter's own retries. Callers must not
	// treat it as a symbol failure (issue #514).
	ErrRateLimited = errors.New("broker: request rate limited")
	// ErrPriceUnavailable is returned when a Quote reports no usable current
	// price (see Quote.HasPrice), so a price-0 bar is never persisted
	// (issue #173).
	ErrPriceUnavailable = errors.New("broker: current price unavailable")
)

// CodedError is implemented by adapter errors that carry a broker-specific
// error code (kabu: *marketdata.APIError).
type CodedError interface {
	error
	// BrokerCode is the broker's error code, 0 when it has none.
	BrokerCode() int
}

// RateLimitedError is implemented by adapter errors that are a request-limit
// rejection of the broker (kabu: HTTP 429 / 4001006).
type RateLimitedError interface {
	error
	RateLimited() bool
}

// ErrorCode returns the broker-specific code carried by err, or 0.
func ErrorCode(err error) int {
	var coded CodedError
	if errors.As(err, &coded) {
		return coded.BrokerCode()
	}
	return 0
}

// IsRateLimited reports whether err is the broker's request limit: an
// exhausted retry (ErrRateLimited) or a single rate-limit rejection.
func IsRateLimited(err error) bool {
	if errors.Is(err, ErrRateLimited) {
		return true
	}
	var limited RateLimitedError
	return errors.As(err, &limited) && limited.RateLimited()
}

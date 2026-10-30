package gmo

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors. The Unsupported* ones are returned before any HTTP call.
var (
	ErrUnsupportedSymbol    = errors.New("gmo: symbol is not configured")
	ErrUnsupportedInterval  = errors.New("gmo: unsupported candle interval (only 1min and 1day)")
	ErrUnsupportedPriceType = errors.New("gmo: unsupported price type (only BID and ASK)")
	ErrUnsupportedDate      = errors.New("gmo: unsupported candle period (1min candles exist only from 2023-10-28, 1day candles only from the year 2023)")
	// ErrInvalidResponse marks a response that cannot be used (bad JSON, a
	// missing symbol, an unparsable price). It is not retried.
	ErrInvalidResponse = errors.New("gmo: invalid response")
	// ErrRetriesExhausted wraps the last failure once Config.MaxAttempts
	// transient failures happened in a row.
	ErrRetriesExhausted = errors.New("gmo: retries exhausted")
)

// retryableCodes are the API-level error codes (keyed on the code alone, not
// on the HTTP or "status" value) that are worth waiting for: ERR-5003 call
// limit, ERR-5201/5202 maintenance.
var retryableCodes = map[string]bool{
	"ERR-5003": true,
	"ERR-5201": true,
	"ERR-5202": true,
}

// Message is one entry of GMO's error "messages" array.
type Message struct {
	Code    string `json:"message_code"`
	Message string `json:"message_string"`
}

// APIError is a failure reported by GMO: a non-200 HTTP status, or an
// HTTP 200 whose "status" is not 0.
type APIError struct {
	HTTPStatus int
	// Status is the API-level "status" field (0 = success); 0 when the body
	// was not GMO's JSON.
	Status   int
	Messages []Message
	// RetryAfter is the server's Retry-After wait (delta-seconds or
	// HTTP-date), capped at MaxRetryAfter; 0 when absent or unusable.
	RetryAfter time.Duration
}

// Error implements error.
func (e *APIError) Error() string {
	var parts []string
	for _, m := range e.Messages {
		parts = append(parts, m.Code+": "+m.Message)
	}
	msg := fmt.Sprintf("gmo: api error (http %d, status %d)", e.HTTPStatus, e.Status)
	if len(parts) > 0 {
		msg += ": " + strings.Join(parts, "; ")
	}
	return msg
}

// Retryable reports whether waiting and retrying can help: HTTP 429 and 5xx,
// or one of the retryable API codes. Anything else (other 4xx, invalid
// symbol/interval/date, unknown codes) is permanent.
func (e *APIError) Retryable() bool {
	if e.HTTPStatus == http.StatusTooManyRequests || e.HTTPStatus >= http.StatusInternalServerError {
		return true
	}
	for _, m := range e.Messages {
		if retryableCodes[m.Code] {
			return true
		}
	}
	return false
}

// transportError is a failure to get a complete response (connection error,
// timeout, truncated body). Always retryable.
type transportError struct{ err error }

func (e *transportError) Error() string { return "gmo: transport: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var te *transportError
	if errors.As(err, &te) {
		return true
	}
	var ae *APIError
	return errors.As(err, &ae) && ae.Retryable()
}

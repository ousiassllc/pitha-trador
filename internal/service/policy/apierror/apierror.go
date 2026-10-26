// Package apierror names why a Jev Trader call failed, as the fixed code the
// Policy Engine writes after "api_error:" in a NONE signal's reject_reason
// (issue #912). The reason of a failed call stays in the same reject_reason as
// before; the code makes the failures countable by kind (timeout, a 5xx, an
// invalid response, ...) without reading the error text, which a client
// outside the database never sees (internal/service/paperresultsmcp/query).
package apierror

import (
	"context"
	"errors"
	"net"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// The codes of Kind. Codes is the complete list: a reader maps any other word
// to Other.
const (
	// Timeout: the API did not answer in time (the per-attempt HTTP timeout, or
	// the call's own deadline).
	Timeout = "timeout"
	// Network: the request never got an answer for another reason than time
	// (connection refused or reset, no such host, ...).
	Network = "network"
	// HTTP5xx: the API answered 5xx (529, overloaded, included).
	HTTP5xx = "http_5xx"
	// HTTP429: the API answered 429, rate limited.
	HTTP429 = "http_429"
	// HTTP4xxOther: the API answered another 4xx (401, 422, ...), which retrying
	// cannot fix.
	HTTP4xxOther = "http_4xx_other"
	// InvalidResponse: a 200 whose body is not a valid answer set
	// (systemone.ErrInvalidResponse).
	InvalidResponse = "invalid_response"
	// PersistFailed: Jev answered but the answer could not be written to
	// jev_decisions (jev.ErrPersistDecision).
	PersistFailed = "persist_failed"
	// Other: anything else (the live prompt version could not be resolved, the
	// state could not be encoded, the call was cancelled, ...).
	Other = "other"
)

// Codes lists every code, in the order the documentation names them.
var Codes = []string{Timeout, Network, HTTP5xx, HTTP429, HTTP4xxOther, InvalidResponse, PersistFailed, Other}

// Kind returns the code of err, an error of jev.Trader.Evaluate.
func Kind(err error) string {
	var api *jev.APIError
	var netErr net.Error
	switch {
	case errors.Is(err, systemone.ErrInvalidResponse):
		return InvalidResponse
	case errors.Is(err, jev.ErrPersistDecision):
		return PersistFailed
	case errors.As(err, &api):
		switch {
		case api.StatusCode == 429:
			return HTTP429
		case api.StatusCode >= 500:
			return HTTP5xx
		}
		return HTTP4xxOther
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return Timeout
	case errors.As(err, &netErr):
		return Network
	}
	return Other
}

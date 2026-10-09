package tachibana

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoData is returned for a symbol the broker reports nothing for (an
// unknown or delisted code, or an empty 時価 row). It is a per-symbol
// outcome: it never extends the market_data_down streak.
var ErrNoData = errors.New("tachibana: no data for the symbol")

// ErrDocumentsUnread is returned by a login the broker accepted but answered
// without virtual URLs because the 金商法交付書面 etc. are unread
// (sKinsyouhouMidokuFlg=1). The operator confirms them on the 標準Web.
var ErrDocumentsUnread = errors.New("tachibana: unread documents, no virtual URLs issued")

// p_errno values of the REQUEST I/F envelope (docs/architecture/overview/
// integrations.md §5.3). 0 is success.
const (
	ErrnoBadArgument    = -1
	ErrnoBusy           = -2
	ErrnoBusyOverload   = -3
	ErrnoSessionExpired = 2
	ErrnoSequence       = 6
	ErrnoClock          = 8
	ErrnoHalted         = 9
	ErrnoHaltedService  = -12
	ErrnoOutOfHours     = -62
)

// ErrorKind is the broker-neutral class of an APIError.
type ErrorKind int

const (
	// KindOther is a control error with no specific handling.
	KindOther ErrorKind = iota
	// KindSessionExpired (p_errno=2): the virtual URL is no longer valid.
	KindSessionExpired
	// KindOutOfHours (p_errno=-62): the broker is closed (03:30〜05:30).
	KindOutOfHours
	// KindBusy (p_errno=-2/-3): the broker is congested.
	KindBusy
	// KindHalted (p_errno=9/-12): the service is stopped.
	KindHalted
	// KindBadArgument (p_errno=-1): the request itself was rejected.
	KindBadArgument
	// KindSequence (p_errno=6): p_no did not exceed the previous one. A
	// numbering bug, never fixed by resending.
	KindSequence
	// KindClock (p_errno=8): p_sd_date is more than 30 seconds off the
	// server's time; the PC clock needs NTP.
	KindClock
	// KindBusiness is a business error (sResultCode != 0) on a request whose
	// envelope was fine.
	KindBusiness
)

// APIError is a failure the broker reported in a response body: a control
// error (Errno = p_errno) or a business error (ResultCode = sResultCode).
// Text is the broker's own message and never contains a virtual URL.
type APIError struct {
	Errno      int
	ResultCode int
	Text       string
}

func (e *APIError) Error() string {
	text := e.Text
	if text == "" {
		text = "no message"
	}
	if e.Errno != 0 {
		return fmt.Sprintf("tachibana: p_errno=%d: %s", e.Errno, text)
	}
	return fmt.Sprintf("tachibana: sResultCode=%d: %s", e.ResultCode, text)
}

// Kind classifies the error.
func (e *APIError) Kind() ErrorKind {
	switch e.Errno {
	case 0:
		if e.ResultCode != 0 {
			return KindBusiness
		}
		return KindOther
	case ErrnoSessionExpired:
		return KindSessionExpired
	case ErrnoOutOfHours:
		return KindOutOfHours
	case ErrnoBusy, ErrnoBusyOverload:
		return KindBusy
	case ErrnoHalted, ErrnoHaltedService:
		return KindHalted
	case ErrnoBadArgument:
		return KindBadArgument
	case ErrnoSequence:
		return KindSequence
	case ErrnoClock:
		return KindClock
	}
	return KindOther
}

// BrokerCode implements broker.CodedError: p_errno, or sResultCode when the
// envelope was fine.
func (e *APIError) BrokerCode() int {
	if e.Errno != 0 {
		return e.Errno
	}
	return e.ResultCode
}

// RateLimited implements broker.RateLimitedError: the broker's congestion
// answer (p_errno=-2/-3) is its closest equivalent of a request limit.
func (e *APIError) RateLimited() bool { return e.Kind() == KindBusy }

// HTTPStatusError is a non-200 HTTP answer. It carries no URL.
type HTTPStatusError struct{ Status int }

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("tachibana: HTTP status %d", e.Status) }

// countsAsFeedFailure reports whether err is evidence the market data feed
// itself is down, as opposed to one request's or symbol's problem (the
// market_data_down streak). HTTP/transport failures, congestion/stop answers
// and a dead session count; the closed-hours answer (-62), a rejected single
// request and per-symbol "no data" do not (issue #532's reasoning). A
// canceled caller is no evidence either way.
func countsAsFeedFailure(err error) bool {
	var apiErr *APIError
	switch {
	case err == nil:
		return false
	case errors.Is(err, context.Canceled), errors.Is(err, ErrNoData):
		return false
	case errors.As(err, &apiErr):
		switch apiErr.Kind() {
		case KindOutOfHours, KindBadArgument, KindBusiness:
			return false
		}
	}
	return true
}

// countsAsBrokerAPIError reports whether err is an HTTP 5xx answer, the
// broker_api_error streak's definition (same as the kabu adapter's).
func countsAsBrokerAPIError(err error) bool {
	var httpErr *HTTPStatusError
	return errors.As(err, &httpErr) && httpErr.Status >= 500
}

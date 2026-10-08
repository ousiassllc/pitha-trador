package marketdata

import (
	"errors"
	"fmt"
	"net/http"
)

// CodeAPIRateLimit is kabuステーションAPI 4001006「API実行回数エラー」
// (https://kabucom.github.io/kabusapi/ptal/error.html). HTTP 429 carries it.
const CodeAPIRateLimit = 4001006

// IsRateLimit reports whether err is a 429 or 4001006 overflow.
func IsRateLimit(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.RateLimited()
}

// APIError represents a kabuステーションAPI error response
// (kabu_STATION_API.yaml components.schemas.ErrorResponse: {"Code": int,
// "Message": string}), or a successful HTTP response whose body still
// reports a non-zero ResultCode (e.g. TokenSuccess.ResultCode, per the
// spec "0が成功。それ以外はエラーコード").
type APIError struct {
	// StatusCode is the HTTP status of the response that carried this
	// error (200 for a body-level ResultCode failure).
	StatusCode int
	// Code is the kabuステーションAPI error/result code.
	Code int
	// Message is the kabuステーションAPI error message, if the API
	// returned one (empty for a body-level ResultCode failure, which has
	// no accompanying message field).
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("marketdata: kabu station api error (http %d, code %d)", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("marketdata: kabu station api error (http %d, code %d): %s", e.StatusCode, e.Code, e.Message)
}

// BrokerCode implements broker.CodedError.
func (e *APIError) BrokerCode() int { return e.Code }

// RateLimited implements broker.RateLimitedError: a 429 or 4001006 overflow.
func (e *APIError) RateLimited() bool {
	return e.Code == CodeAPIRateLimit || e.StatusCode == http.StatusTooManyRequests
}

package marketdata

import (
	"errors"
	"fmt"
)

// ErrPriceUnavailable is returned by callers that need a usable last price
// when a Board reports none (CurrentPrice 0/NaN: before the opening auction,
// no trade yet, or null in the response). See Board.HasPrice.
var ErrPriceUnavailable = errors.New("marketdata: current price unavailable")

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

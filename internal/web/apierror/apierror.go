// Package apierror installs the project-wide huma.NewError override for the
// `/api/v1` JSON API (issue #215).
//
// Huma's default NewError copies every err.Error() into
// errors[].message, which would leak SQLite messages and internal paths
// from `huma.Error500InternalServerError("…", err)` call sites. The SSR
// side already answers with a fixed message and logs the cause
// (shared.RespondPageError, #143); this package gives the JSON API the
// same contract.
package apierror

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
)

var installOnce sync.Once

// Install replaces huma.NewError with NewError. It is idempotent and must
// run before the first request is served (registerAPI calls it while
// building the router).
func Install() {
	installOnce.Do(func() { huma.NewError = NewError })
}

// NewError builds the error body for status and msg.
//
// For 4xx the wrapped errs are kept as errors[] details exactly like
// Huma's default (validation messages such as "expected integer" are the
// client-facing contract). For 5xx the errs are logged with slog and
// dropped from the response, so the client only sees the fixed msg.
func NewError(status int, msg string, errs ...error) huma.StatusError {
	if status >= http.StatusInternalServerError {
		slog.Error("api: internal error", "status", status, "message", msg, "error", errors.Join(errs...))
		errs = nil
	}
	details := make([]*huma.ErrorDetail, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		if detailer, ok := err.(huma.ErrorDetailer); ok {
			details = append(details, detailer.ErrorDetail())
			continue
		}
		details = append(details, &huma.ErrorDetail{Message: err.Error()})
	}
	return &huma.ErrorModel{
		Status: status,
		Title:  http.StatusText(status),
		Detail: msg,
		Errors: details,
	}
}

package system

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// ErrorLogExporter is the subset of internal/logging.Exporter the error log
// download needs (FR-ERRLOG-2). An interface keeps the handler testable
// without log files on disk.
type ErrorLogExporter interface {
	Export(ctx context.Context, days int, minLevel slog.Level) (logging.ExportResult, error)
}

// ErrErrorLogExporterNotConfigured is what UnconfiguredErrorLogExporter
// returns.
var ErrErrorLogExporterNotConfigured = errors.New("system: error log exporter not configured (missing router.WithErrorLogExporter)")

// UnconfiguredErrorLogExporter fails every export, used as
// internal/router.New()'s default until a real *logging.Exporter is wired
// in: an entry point that forgets router.WithErrorLogExporter gets a 500
// (cause in the slog) instead of an empty 200 download that looks like "no
// errors".
type UnconfiguredErrorLogExporter struct{}

func (UnconfiguredErrorLogExporter) Export(context.Context, int, slog.Level) (logging.ExportResult, error) {
	return logging.ExportResult{}, ErrErrorLogExporterNotConfigured
}

// ErrorLogHandler implements `GET /api/v1/logs/errors`
// (docs/api/endpoints/huma-api.md, FR-ERRLOG-1〜7): the Settings screen's
// `#error-log-panel` form downloads the masked slog records as an NDJSON
// attachment.
type ErrorLogHandler struct {
	exporter ErrorLogExporter
	now      func() time.Time
}

// NewErrorLogHandler returns an ErrorLogHandler backed by exporter.
func NewErrorLogHandler(exporter ErrorLogExporter) *ErrorLogHandler {
	return &ErrorLogHandler{exporter: exporter, now: time.Now}
}

// errorLogLevelWarn is ErrorLogInput.Level's non-default value.
const errorLogLevelWarn = "warn"

// ErrorLogInput is `GET /api/v1/logs/errors`'s query. Huma rejects values
// outside the declared range/enum with 422 (FR-ERRLOG-5).
type ErrorLogInput struct {
	Days  int    `query:"days" default:"7" minimum:"1" maximum:"90" doc:"Number of most recent UTC days to export, today included."`
	Level string `query:"level" default:"error" enum:"error,warn" doc:"error = ERROR only; warn = WARN and ERROR."`
}

// ErrorLogOutput is the attachment response. Truncated stays empty (header
// omitted) unless older records were dropped to fit the size cap.
type ErrorLogOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	RecordCount        int    `header:"X-Pitha-Record-Count" doc:"Number of records in the body."`
	Truncated          string `header:"X-Pitha-Truncated" doc:"\"true\" when older records were dropped to fit 10 MiB."`
	Body               []byte
}

// APIErrorLogs implements `GET /api/v1/logs/errors`. Unlike the other safe
// `/api/v1` GETs it needs the valid session cookie (FR-ERRLOG-6): the logs
// are not something to hand to a cookie-less caller, and the cookie also
// makes the download count as operator activity (middleware.Heartbeat).
func (h *ErrorLogHandler) APIErrorLogs(ctx context.Context, in *ErrorLogInput) (*ErrorLogOutput, error) {
	if !middleware.Authenticated(ctx) {
		return nil, huma.Error403Forbidden("missing or invalid session cookie")
	}
	minLevel := slog.LevelError
	if in.Level == errorLogLevelWarn {
		minLevel = slog.LevelWarn
	}
	res, err := h.exporter.Export(ctx, in.Days, minLevel)
	if err != nil {
		return nil, huma.Error500InternalServerError("export error log failed", err)
	}
	out := &ErrorLogOutput{
		ContentType:        "application/x-ndjson; charset=utf-8",
		ContentDisposition: `attachment; filename="pitha-error-logs-` + h.now().UTC().Format("20060102-150405") + `.ndjson"`,
		RecordCount:        res.Records,
		Body:               res.Data,
	}
	if res.Truncated {
		out.Truncated = "true"
	}
	return out, nil
}

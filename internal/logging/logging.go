// Package logging configures the process-wide slog.Default logger
// non-functional.md §5 requires: structured JSON output, written through
// a daily-rotating file (rotate.go) so log/slog call sites elsewhere in
// the codebase (scheduler, marketdata, jev, web/middleware, ...) need no
// changes of their own to become §5.1-compliant.
//
// cmd/desktop and cmd/server (the two process entrypoints,
// docs/architecture/overview.md §3/§9) each call New once at startup and
// pass the result to slog.SetDefault.
package logging

import (
	"io"
	"log/slog"
)

// New returns a JSON slog.Logger writing to w at the given minimum level.
// w is typically a *RotatingWriter (rotate.go) so output also satisfies
// §5's daily-rotation requirement, but any io.Writer works (e.g. os.Stdout
// for local `go run` sessions without a log directory).
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// Package logging configures the process-wide slog.Default logger
// non-functional.md §5 requires: structured JSON output, written through
// a daily-rotating file (rotate.go) so log/slog call sites elsewhere in
// the codebase (scheduler, marketdata, jev, web/middleware, ...) need no
// changes of their own to become §5.1-compliant.
//
// cmd/desktop and cmd/server (the two process entrypoints,
// docs/architecture/overview.md §3/§9) each call New (or NewSplit) once at
// startup and pass the result to slog.SetDefault.
package logging

import (
	"context"
	"errors"
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

// NewSplit is New plus a second sink: every record at level or above goes
// to all (the full log), and the records at slog.LevelError or above also
// go to errs (the ERROR-only log), so a failure can be found without
// grepping through the full log. errs is typically a RotatingWriter from
// NewErrorRotatingWriter.
func NewSplit(all, errs io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(splitHandler{
		all:  slog.NewJSONHandler(all, &slog.HandlerOptions{Level: level}),
		errs: slog.NewJSONHandler(errs, &slog.HandlerOptions{Level: slog.LevelError}),
	})
}

// splitHandler fans a record out to the full-log handler and, for ERROR
// and above, the error-log handler. Each handler applies its own level.
type splitHandler struct {
	all, errs slog.Handler
}

func (h splitHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.all.Enabled(ctx, l) || h.errs.Enabled(ctx, l)
}

func (h splitHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if h.all.Enabled(ctx, r.Level) {
		err = h.all.Handle(ctx, r.Clone())
	}
	if h.errs.Enabled(ctx, r.Level) {
		err = errors.Join(err, h.errs.Handle(ctx, r))
	}
	return err
}

func (h splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return splitHandler{all: h.all.WithAttrs(attrs), errs: h.errs.WithAttrs(attrs)}
}

func (h splitHandler) WithGroup(name string) slog.Handler {
	return splitHandler{all: h.all.WithGroup(name), errs: h.errs.WithGroup(name)}
}

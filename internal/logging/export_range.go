package logging

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// MetricsLevel is the minimum level of the metrics export: INFO, the level
// the measurement records (requirements/non-functional.md §5.1, e.g.
// "rankingmeasure: ranking measured", "rankingwatch: watch list updated")
// are written at, so the export carries them next to the WARN/ERROR records.
const MetricsLevel = slog.LevelInfo

// maxExportRangeDays bounds ExportRange's span, so a typo cannot make it
// look up thousands of day files.
const maxExportRangeDays = 3660

// ExportRange returns the records of the full log whose record time lies in
// [from, to) and whose level is at least minLevel, masked and in time order
// (docs/architecture/overview/flows.md §10.6). It is Export for an explicit
// period: the day files covering the period are read (same UTC-date file
// names, plain or .log.gz) and each record is kept by its own time. A record
// without a parsable time is placed at the previous record's time. The
// MaxExportBytes cap and Truncated work as in Export. A period of more than
// maxExportRangeDays days is an error.
func (e *Exporter) ExportRange(ctx context.Context, from, to time.Time, minLevel slog.Level) (ExportResult, error) {
	if !to.After(from) {
		return ExportResult{}, fmt.Errorf("logging: export range end %s must be after start %s", to.Format(time.RFC3339), from.Format(time.RFC3339))
	}
	first := truncateToUTCDay(from)
	last := truncateToUTCDay(to.Add(-time.Nanosecond))
	if last.Sub(first) > time.Duration(maxExportRangeDays)*24*time.Hour {
		return ExportResult{}, fmt.Errorf("logging: export range spans more than %d days", maxExportRangeDays)
	}
	var names []string // newest day first
	for day := last; !day.Before(first); day = day.AddDate(0, 0, -1) {
		names = append(names, day.Format(dailyFileLayout))
	}
	return e.export(ctx, names, minLevel, &timeWindow{from: from, to: to})
}

// ExportMetrics is ExportRange at MetricsLevel: the metrics (INFO and above)
// records of [from, to).
func (e *Exporter) ExportMetrics(ctx context.Context, from, to time.Time) (ExportResult, error) {
	return e.ExportRange(ctx, from, to, MetricsLevel)
}

func truncateToUTCDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// timeWindow is the half-open record-time period [from, to) of ExportRange.
type timeWindow struct{ from, to time.Time }

func (w *timeWindow) contains(at time.Time) bool {
	return !at.IsZero() && !at.Before(w.from) && at.Before(w.to)
}

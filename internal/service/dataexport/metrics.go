package dataexport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/logging"
)

// MetricsSource returns the measurement (INFO and above) log records of
// [from, to) as masked NDJSON (internal/logging.Exporter).
type MetricsSource interface {
	ExportMetrics(ctx context.Context, from, to time.Time) (logging.ExportResult, error)
}

// Metrics is the measurement-log Collector (KindMetrics): one `metrics` table
// with a row per INFO-and-above slog record of the daily JSON log (#652
// ranking measurements, watch-list updates, ... and the WARN/ERROR records
// next to them), in time order. The logging.Exporter masks every record and
// keeps its 10 MiB cap; when older records were dropped to fit it the
// manifest file entry says `truncated`.
type Metrics struct {
	Source MetricsSource
}

// Kind implements Collector.
func (Metrics) Kind() Kind { return KindMetrics }

// Tables implements Collector. Each call returns a table with its own
// truncation state, so concurrent exports do not share it.
func (m Metrics) Tables() []Table {
	var truncated bool
	columns := cols("time", "level", "msg", "record")
	columns[3].JSON = true // the whole slog record: its attributes are the measurements
	return []Table{{
		Name:    "metrics",
		Columns: columns,
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			res, err := m.Source.ExportMetrics(ctx, p.From, p.To)
			if err != nil {
				return err
			}
			truncated = res.Truncated
			for rest := res.Data; len(rest) > 0; {
				line := rest
				if i := bytes.IndexByte(rest, '\n'); i >= 0 {
					line, rest = rest[:i], rest[i+1:]
				} else {
					rest = nil
				}
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				row, err := metricsRow(line)
				if err != nil {
					return err
				}
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
		Truncated: func() bool { return truncated },
	}}
}

// metricsRow splits one exported record into its time, level and message,
// keeping the record itself (all attributes) in the last column.
func metricsRow(line []byte) (Row, error) {
	var rec struct {
		Time  string `json:"time"`
		Level string `json:"level"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, fmt.Errorf("dataexport: metrics record is not JSON: %w", err)
	}
	var at any = rec.Time
	if t, err := time.Parse(time.RFC3339Nano, rec.Time); err == nil {
		at = t
	}
	return Row{at, rec.Level, rec.Msg, string(line)}, nil
}

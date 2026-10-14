// Package dataexport builds the local export archive of the trade log, the
// decision log and the measurement log (issues #768/#770/#771/#772/#773):
// for a period and a set of kinds it writes a ZIP holding one CSV or JSONL
// file per table plus manifest.json
// (schema version, period, per-kind row counts).
//
// Secrets (tokens, passwords, API keys, 第二暗証番号, ...) are masked on every
// value before it is encoded, with the log export's rules (internal/logging
// MaskString / MaskJSONText / IsSensitiveKey), so a collector cannot leak one
// by forgetting to. The archive is written to the caller's io.Writer only:
// the package opens no network connection and has no sharing or sending path
// (立花証券の利用規約に配慮し、出力はローカル保存のみ).
//
// The export covers Paper trading only (#55 stays blocked).
package dataexport

import (
	"context"
	"time"
)

// Kind is one export category the caller can select.
type Kind string

const (
	// KindTrades is the trade log: orders, fills, closes and PnL.
	KindTrades Kind = "trades"
	// KindDecisions is the decision log: Jev requests/responses, features,
	// skip/reject reasons and after-the-fact labels, linked by decision_id.
	KindDecisions Kind = "decisions"
	// KindMetrics is the measurement log: the INFO-and-above records of the
	// daily JSON log (issue #772).
	KindMetrics Kind = "metrics"
)

// Period is the half-open time range [From, To) an export covers.
type Period struct {
	From time.Time
	To   time.Time
}

// Row is one table row: values aligned with Table.Columns. Allowed values are
// nil, string, bool, any signed/unsigned integer, float32/float64,
// time.Time (written as RFC3339 UTC) and named types of those kinds.
type Row []any

// Column names one table column. JSON marks a string holding a JSON document
// (e.g. a stored Jev request/response): it is masked as JSON, and written as
// a nested document in JSONL when it is valid JSON.
type Column struct {
	Name string
	JSON bool
}

// Table is one file of the archive: a name, its columns and the read that
// streams its rows.
type Table struct {
	Name    string
	Columns []Column
	// Read calls emit for every row of the period, in a stable order,
	// without holding the period in memory, and returns the first error
	// (including emit's).
	Read func(ctx context.Context, p Period, emit func(Row) error) error
	// Truncated, when set, reports after Read whether the source dropped
	// rows of the period to fit its own size cap; the manifest records it.
	Truncated func() bool
}

// Collector supplies the tables of one Kind. The first table is the kind's
// primary table: its row count is the kind's row count in the manifest.
type Collector interface {
	Kind() Kind
	Tables() []Table
}

func cols(names ...string) []Column {
	out := make([]Column, len(names))
	for i, n := range names {
		out[i] = Column{Name: n}
	}
	return out
}

// val dereferences an optional value; nil stays nil (a NULL column).
func val[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

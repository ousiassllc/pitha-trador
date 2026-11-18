package datasetexport

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// row is one decision of the dataset: its cells in Columns order and what the split needs.
type row struct {
	id int64
	at time.Time
	// day is the decision's JST date.
	day string
	// windowEnd is the end of the longest label window of the row (see windowEnd): the
	// decision time when the row has no label.
	windowEnd time.Time
	cells     []string
}

// scope is the decisions every query of the export is bounded by: the trader decisions of
// [from, to), all directions, served by jev_decisions_type_timestamp_idx.
const scope = ` d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?`

// readDecisions reads the trader decisions of [from, to) with their decision-time values and
// the derived features of their own bar, oldest first (timestamp, then id). More than
// MaxDecisions is query.ErrLimitExceeded. The market_snapshots and instruments joins are on
// unique keys; every other source is batched per call (read_labels.go, read_answers.go).
func readDecisions(ctx context.Context, q query.Q, from, to string) ([]*row, error) {
	selects := func(fields []field) string {
		sqls := make([]string, len(fields))
		for i, f := range fields {
			sqls[i] = f.selectSQL()
		}
		return strings.Join(sqls, ",\n\t")
	}
	stmt := `
SELECT d.id, d.instrument_id, d.symbol, d.timestamp, d.question_version, d.model_id,
	` + selects(decisionFields) + `,
	` + selects(barFields) + `,
	m.id IS NOT NULL,
	` + selects(instrumentFields) + `
FROM jev_decisions d
LEFT JOIN market_snapshots m ON m.instrument_id = d.instrument_id AND m.timestamp = d.timestamp
LEFT JOIN instruments i ON i.id = d.instrument_id
WHERE` + scope + `
ORDER BY d.timestamp, d.id LIMIT ?`
	rows, err := q.QueryContext(ctx, stmt, from, to, MaxDecisions+1)
	if err != nil {
		return nil, fmt.Errorf("datasetexport: read decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		id, instrumentID              int64
		symbol, ts, version, model    string
		hasBar                        bool
		decSlots, barSlots, instSlots = slotsOf(decisionFields), slotsOf(barFields), slotsOf(instrumentFields)
		out                           []*row
	)
	dest := []any{&id, &instrumentID, &symbol, &ts, &version, &model}
	dest = append(append(append(dest, decSlots...), barSlots...), &hasBar)
	dest = append(dest, instSlots...)
	for rows.Next() {
		if len(out) == MaxDecisions {
			return nil, query.ErrLimitExceeded
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("datasetexport: scan decision: %w", err)
		}
		at, err := sqlutil.ParseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("datasetexport: decision %d has an unreadable timestamp", id)
		}
		r := &row{id: id, at: at, day: at.In(query.JST).Format(time.DateOnly), windowEnd: at, cells: make([]string, off.total)}
		r.cells[cDecisionID] = strconv.FormatInt(id, 10)
		r.cells[cInstrumentID] = strconv.FormatInt(instrumentID, 10)
		r.cells[cSymbol] = symbol
		r.cells[cDecidedAt] = sqlutil.FormatTime(at)
		r.cells[cDecisionDay] = r.day
		r.cells[cQuestionVersion] = version
		r.cells[cModelID] = model
		for _, g := range []struct {
			start int
			slots []any
		}{{off.decision, decSlots}, {off.bar, barSlots}, {off.instrument, instSlots}} {
			for i, s := range g.slots {
				r.cells[g.start+i] = cellOf(s)
			}
		}
		r.cells[off.hasBar] = boolCell(hasBar)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("datasetexport: read decisions: %w", err)
	}
	return out, nil
}

// slotsOf makes the scan destinations of fields: a number scans as a float, anything else as text.
func slotsOf(fields []field) []any {
	slots := make([]any, len(fields))
	for i, f := range fields {
		if f.kind == kindNum {
			slots[i] = new(sql.NullFloat64)
		} else {
			slots[i] = new(sql.NullString)
		}
	}
	return slots
}

// cellOf renders a scanned column: NULL is empty, a number is a plain decimal.
func cellOf(slot any) string {
	switch v := slot.(type) {
	case *sql.NullFloat64:
		if v.Valid {
			return strconv.FormatFloat(v.Float64, 'f', -1, 64)
		}
	case *sql.NullString:
		if v.Valid {
			return v.String
		}
	}
	return ""
}

func boolCell(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

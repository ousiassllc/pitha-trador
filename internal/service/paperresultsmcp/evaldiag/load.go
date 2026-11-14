package evaldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// settingsReader is a read-only runtime_settings view limited to the keys the
// label and the cutoff need (barrier, cost and metalabel.*): runtime_settings
// can hold secret paths, so any other key is refused before SQL runs. It is the
// Store barriersettings, costsettings and metalabelsettings load from, so the
// effective values are the app's own.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !barriersettings.IsKey(key) && !costsettings.IsKey(key) && !slices.Contains(metalabelsettings.Keys(), key) {
		return "", false, fmt.Errorf("evaldiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("evaldiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// decisionRow is one trader LONG/SHORT decision with what the arms need besides
// the bars after it.
type decisionRow struct {
	id               int64
	at, responseAt   time.Time
	direction        string
	modelID, version string
	// hasBar tells a market_snapshots bar exists at the decision's own timestamp;
	// return5m is that bar's return_5m (NULL: nil).
	hasBar   bool
	return5m *float64
	// storedNet is the stored label's net_return_bps (nil: none).
	storedNet *float64
	// entered: a trade signal of the decision passed Risk.
	entered bool
}

// scope is the SQL the decision list and the bar stream share: the population
// (trader LONG/SHORT of [from, to), whatever happened to them afterwards: no
// trade_signals or Risk filter) and the optional model / prompt version.
func scope(from, to string, req Request) (string, []any) {
	cond, args := ` d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?`, []any{from, to}
	if req.PromptVersion != "" {
		cond += " AND d.question_version = ?"
		args = append(args, req.PromptVersion)
	}
	if req.ModelID != "" {
		cond += " AND d.model_id = ?"
		args = append(args, req.ModelID)
	}
	return cond, args
}

// listDecisions reads the decisions of scope with the stored label at horizon,
// the entry flag and the return_5m of their own bar, oldest first. More than
// MaxDecisions is query.ErrLimitExceeded.
func listDecisions(ctx context.Context, q query.Q, from, to string, horizon int, req Request) ([]decisionRow, error) {
	cond, args := scope(from, to, req)
	args = append([]any{domain.CalibrationLabelVersionTripleBarrier, horizon}, append(args, MaxDecisions+1)...)
	stmt := `
SELECT d.id, d.timestamp, d.created_at, d.direction, d.model_id, d.question_version,
	m.id IS NOT NULL, m.return_5m, o.net_return_bps,
	EXISTS (SELECT 1 FROM trade_signals s WHERE s.jev_decision_id = d.id AND s.risk_passed = 1)
FROM jev_decisions d
LEFT JOIN market_snapshots m ON m.instrument_id = d.instrument_id AND m.timestamp = d.timestamp
LEFT JOIN calibration_outcomes o ON o.jev_decision_id = d.id AND o.label_version = ? AND o.horizon_minutes = ?
	AND o.net_return_bps IS NOT NULL
WHERE` + cond + `
ORDER BY d.timestamp, d.id LIMIT ?`
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("query: list evaluated decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []decisionRow
	for rows.Next() {
		var (
			d             decisionRow
			at, createdAt string
			ret, net      sql.NullFloat64
		)
		if err := rows.Scan(&d.id, &at, &createdAt, &d.direction, &d.modelID, &d.version, &d.hasBar, &ret, &net, &d.entered); err != nil {
			return nil, fmt.Errorf("query: scan evaluated decision: %w", err)
		}
		if len(out) == MaxDecisions {
			return nil, query.ErrLimitExceeded
		}
		if d.at, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		if d.responseAt, err = sqlutil.ParseTime(createdAt); err != nil {
			return nil, err
		}
		if d.responseAt.Before(d.at) { // calibration.ResponseTime: never earlier than the decision's bar
			d.responseAt = d.at
		}
		if ret.Valid {
			d.return5m = &ret.Float64
		}
		if net.Valid {
			d.storedNet = &net.Float64
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: list evaluated decisions: %w", err)
	}
	return out, nil
}

// barStream is the bars of every decision of scope in one statement (a join on
// the instrument and timestamp indexes, not one statement per decision, so the
// tool stays inside the MCP's per-call time budget): for each decision the bars
// from its response time (the Labeler's ResponseTime) up to EntryWaitTolerance +
// horizon + 1 s after it, which is the Labeler's own window. It yields the rows
// in the decision list's order (timestamp, id), so window(id) is a merge.
type barStream struct {
	rows *sql.Rows
	ok   bool  // cur is a row
	id   int64 // cur's decision
	cur  *domain.Snapshot
}

func openBars(ctx context.Context, q query.Q, from, to string, horizon int, req Request) (*barStream, error) {
	cond, args := scope(from, to, req)
	seconds := int((calibration.EntryWaitTolerance + time.Duration(horizon)*time.Minute + time.Second) / time.Second)
	// lo is ResponseTime (the later of created_at and timestamp; stored fixed-width, so the
	// text order is the time order); hi is lo + the window, in the stored width. Only a
	// bound: LabelWindow trims the path to the timeout itself.
	stmt := `
SELECT d.id, m.timestamp, m.price, m.bid, m.ask
FROM jev_decisions d
LEFT JOIN market_snapshots m ON m.instrument_id = d.instrument_id
	AND m.timestamp >= MAX(d.created_at, d.timestamp)
	AND m.timestamp < strftime('%Y-%m-%dT%H:%M:%f', MAX(d.created_at, d.timestamp), '+' || ? || ' seconds') || '000000Z'
WHERE` + cond + `
ORDER BY d.timestamp, d.id, m.timestamp`
	rows, err := q.QueryContext(ctx, stmt, append([]any{seconds}, args...)...) //nolint:rowserrcheck // rows.Err is checked by barStream.advance when the stream ends
	if err != nil {
		return nil, fmt.Errorf("query: list evaluation bars: %w", err)
	}
	s := &barStream{rows: rows}
	return s, s.advance()
}

func (s *barStream) close() { _ = s.rows.Close() }

// advance reads the next row into cur.
func (s *barStream) advance() error {
	if s.ok = s.rows.Next(); !s.ok {
		if err := s.rows.Err(); err != nil {
			return fmt.Errorf("query: list evaluation bars: %w", err)
		}
		return nil
	}
	var (
		ts       sql.NullString
		price    sql.NullFloat64
		bid, ask sql.NullFloat64
	)
	if err := s.rows.Scan(&s.id, &ts, &price, &bid, &ask); err != nil {
		return fmt.Errorf("query: scan evaluation bar: %w", err)
	}
	s.cur = nil // a decision without a bar in its window
	if !ts.Valid {
		return nil
	}
	bar := domain.Snapshot{Price: price.Float64}
	var err error
	if bar.Timestamp, err = sqlutil.ParseTime(ts.String); err != nil {
		return err
	}
	if bid.Valid {
		bar.Bid = &bid.Float64
	}
	if ask.Valid {
		bar.Ask = &ask.Float64
	}
	s.cur = &bar
	return nil
}

// window returns the bars of decision id (ascending). The stream and the
// decision list have the same decisions in the same order, so the next group of
// the stream is always the next decision's: window must be called once per
// decision, in list order.
func (s *barStream) window(id int64) ([]domain.Snapshot, error) {
	var out []domain.Snapshot
	for s.ok && s.id == id {
		if s.cur != nil {
			out = append(out, *s.cur)
		}
		if err := s.advance(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

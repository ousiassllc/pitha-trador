// Package evalrecord is the recorder of the daily Jev evaluation record (issue #1089): once
// a JST day is over it evaluates the day's trader LONG/SHORT decisions with the evaluation
// of the jev_evaluation tool (evaldiag.Rows: every arm on the same rows, the app's own
// cost-inclusive triple-barrier label, the settings in force that day) and stores the
// day's main numbers per cohort in jev_evaluation_daily, so the trend over weeks can be
// read (jev_evaluation_trend) although the label settings, the model and the question
// version change.
//
// The record is of the day it was written: a row is never rewritten by a later run
// (INSERT OR IGNORE on the unique key), so changing a setting afterwards does not
// change history. Everything here is research only: nothing is written but
// jev_evaluation_daily and nothing it holds feeds Jev, Risk, an order or the meta-model.
package evalrecord

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// WindowDays is how many JST days back from today (today included) a Run looks for
// days to record: a day older than that that was never recorded stays unrecorded.
const WindowDays = 7

// PurgeBatchRows is how many rows one DELETE removes, and DefaultMaxPurgeBatches how
// many such deletes one Run makes before it reports More and leaves the rest to the
// next Run.
const (
	PurgeBatchRows         = 1000
	DefaultMaxPurgeBatches = 10
)

// jst is the zone of the days the record is kept in; a fixed zone keeps the result
// independent of the host's time zone.
var jst = time.FixedZone("JST", 9*60*60)

// Store is the persistence Recorder needs (internal/repository/evalrecord). Days are
// JST dates ("2006-01-02"), from inclusive and to exclusive.
type Store interface {
	// DecisionDays are the days with trader LONG/SHORT decisions, oldest first.
	DecisionDays(ctx context.Context, from, to string) ([]string, error)
	// RecordedDays are the days that already have rows in jev_evaluation_daily, oldest first.
	RecordedDays(ctx context.Context, from, to string) ([]string, error)
	// InsertDay adds the rows (rows already recorded are left as they are) and returns how many were new.
	InsertDay(ctx context.Context, rows []evalrecord.Row) (int, error)
	// PurgeBefore deletes up to limit rows older than day and returns how many.
	PurgeBefore(ctx context.Context, day string, limit int) (int, error)
}

// RowsSource evaluates the decisions of a range (evaldiag.Rows).
type RowsSource func(ctx context.Context, r query.Range) ([]evaldiag.Row, evaldiag.Meta, error)

// RetentionSource returns how many JST days of rows to keep
// (researchjobsettings.Eval(...).RetentionDays).
type RetentionSource func(ctx context.Context) (days int, err error)

// Result is what one Run did.
type Result struct {
	// Day is the JST day recorded in this run ("" when none was pending) and Rows the new rows of it.
	Day  string
	Rows int
	// Purged is how many old rows this run deleted.
	Purged int
	// More is true when this run stopped with work left (a pending day, rows to purge):
	// call Run again. Every run with More set made progress.
	More bool
}

// Recorder records the JST days that have trader decisions and no row yet, oldest
// first, one day per Run, and deletes the rows older than the retention.
type Recorder struct {
	store     Store
	rows      RowsSource
	retention RetentionSource
	now       func() time.Time
	maxPurge  int
}

// New returns a Recorder over store that evaluates through q (read-only use) with
// the evaluation's own defaults (the barrier timeout in force as the horizon, every
// model and question version).
func New(store Store, q query.Q, retention RetentionSource) *Recorder {
	return &Recorder{
		store: store, retention: retention, now: time.Now, maxPurge: DefaultMaxPurgeBatches,
		rows: func(ctx context.Context, r query.Range) ([]evaldiag.Row, evaldiag.Meta, error) {
			return evaldiag.Rows(ctx, q, r, evaldiag.Request{})
		},
	}
}

// WithClock overrides time.Now (tests).
func (r *Recorder) WithClock(now func() time.Time) *Recorder {
	r.now = now
	return r
}

// WithRows overrides the evaluation (tests).
func (r *Recorder) WithRows(rows RowsSource) *Recorder {
	r.rows = rows
	return r
}

// WithMaxPurgeBatches overrides DefaultMaxPurgeBatches (tests).
func (r *Recorder) WithMaxPurgeBatches(n int) *Recorder {
	r.maxPurge = n
	return r
}

// Run does one bounded chunk: it records the oldest pending JST day (a day of the
// last WindowDays days up to today that has trader LONG/SHORT decisions and no row),
// and, once no day is pending, deletes the rows older than the retention (at most
// maxPurge batches). It returns More while pending days or rows to purge remain.
// A day too large to evaluate (query.ErrLimitExceeded) is an error and writes
// nothing of that day.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	var res Result
	keep, err := r.retention(ctx)
	if err != nil {
		return res, fmt.Errorf("evalrecord: retention: %w", err)
	}
	if keep < 1 {
		return res, fmt.Errorf("evalrecord: retention of %d days is not valid", keep)
	}
	today := r.now().In(jst)
	cutoff := today.AddDate(0, 0, -keep).Format(time.DateOnly)
	// Days before the cutoff are not recorded: the purge would delete them again at once.
	from := max(today.AddDate(0, 0, 1-WindowDays).Format(time.DateOnly), cutoff)
	to := today.AddDate(0, 0, 1).Format(time.DateOnly)

	pending, err := r.pendingDays(ctx, from, to)
	if err != nil {
		return res, err
	}
	if len(pending) > 0 {
		if res.Rows, err = r.recordDay(ctx, pending[0]); err != nil {
			return res, err
		}
		res.Day, res.More = pending[0], len(pending) > 1
	}
	if res.More {
		return res, nil
	}
	for range r.maxPurge {
		n, err := r.store.PurgeBefore(ctx, cutoff, PurgeBatchRows)
		if err != nil {
			return res, err
		}
		res.Purged += n
		if n < PurgeBatchRows {
			return res, nil
		}
	}
	res.More = true // the last batch was full: there may be more
	return res, nil
}

// pendingDays are the days of [from, to) with decisions and no record, oldest first.
func (r *Recorder) pendingDays(ctx context.Context, from, to string) ([]string, error) {
	decided, err := r.store.DecisionDays(ctx, from, to)
	if err != nil {
		return nil, err
	}
	recorded, err := r.store.RecordedDays(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range decided {
		if !slices.Contains(recorded, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// recordDay evaluates the JST day and stores its rows in one transaction.
func (r *Recorder) recordDay(ctx context.Context, day string) (int, error) {
	from, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		return 0, fmt.Errorf("evalrecord: day %q: %w", day, err)
	}
	rows, meta, err := r.rows(ctx, query.Range{From: from, To: from.AddDate(0, 0, 1)})
	if err != nil {
		return 0, fmt.Errorf("evalrecord: evaluate %s: %w", day, err)
	}
	agg, err := Aggregate(day, rows, meta)
	if err != nil {
		return 0, err
	}
	if len(agg) == 0 { // the day was listed for its decisions: no cohort means the two reads disagree, and the day would stay pending forever
		return 0, fmt.Errorf("evalrecord: %s has decisions but the evaluation found no cohort", day)
	}
	return r.store.InsertDay(ctx, agg)
}

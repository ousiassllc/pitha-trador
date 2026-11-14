package researchlabel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
)

// DefaultMaxDaysPerRun bounds how many JST days one Run records, oldest first,
// so the first run over the whole retained history (90 days) does not hog the
// database shared with live trading: the rest follows on the next daily runs.
const DefaultMaxDaysPerRun = 10

// DefaultBatchPause is the pause between two instruments' insert transactions:
// the modernc.org/sqlite busy handler polls at most every 100 ms, so 200 ms lets
// a waiting live writer land inside the pause (as retention's defaultBatchPause).
const DefaultBatchPause = 200 * time.Millisecond

// jst is the zone of the day a label belongs to; a fixed zone keeps the result
// independent of the host's time zone.
var jst = time.FixedZone("JST", 9*60*60)

// Store is the persistence Recorder needs (internal/repository/researchlabel).
// Days are JST dates ("2006-01-02").
type Store interface {
	// PendingDays returns up to limit days before the given day, oldest first,
	// that have market_snapshots rows and no completion mark.
	PendingDays(ctx context.Context, before string, limit int) ([]string, error)
	// InstrumentIDs are the instruments with a bar on day.
	InstrumentIDs(ctx context.Context, day string) ([]int64, error)
	// Bars are one instrument's bars of day, ascending.
	Bars(ctx context.Context, day string, instrumentID int64) ([]domain.Snapshot, error)
	// Insert adds the rows (rows already recorded are left as they are) and
	// returns how many were new. One short transaction.
	Insert(ctx context.Context, rows []rlabel.Label, createdAt time.Time) (int, error)
	// MarkDone records that every instrument of day was labeled.
	MarkDone(ctx context.Context, day string, snapshots int, completedAt time.Time) error
}

// CostSource returns the costs the label assumes at record time
// (CostsFromSettings over the Settings).
type CostSource func(ctx context.Context) (Costs, error)

// Result is what one Run recorded.
type Result struct {
	// Days are the JST days completed in this run.
	Days []string
	// Snapshots are the bars read; Rows the new research_horizon_labels rows.
	Snapshots, Rows int
}

// Recorder writes the research labels of every JST day that is complete (strictly
// before today) and not yet marked done. It is resumable and idempotent: a day
// is marked only after all its instruments are labeled, so a run that stops
// midway is redone from the start of that day, and rows already written are
// kept by UNIQUE (snapshot_id, horizon).
type Recorder struct {
	store   Store
	costs   CostSource
	now     func() time.Time
	maxDays int
	// pause is how long recordDay waits after each instrument's insert so a writer
	// waiting on busy_timeout can take the write lock (WAL has no fair queue;
	// the same reasoning as retention's batch pause, issue #533). 0 = no pause.
	pause time.Duration
}

// NewRecorder returns a Recorder over store, with costs read at the start of
// every Run.
func NewRecorder(store Store, costs CostSource) *Recorder {
	return &Recorder{store: store, costs: costs, now: time.Now, maxDays: DefaultMaxDaysPerRun}
}

// WithClock overrides time.Now (tests).
func (r *Recorder) WithClock(now func() time.Time) *Recorder {
	r.now = now
	return r
}

// WithBatchPause sets the pause between instruments' insert transactions
// (DefaultBatchPause in production; none by default, so tests do not wait).
func (r *Recorder) WithBatchPause(d time.Duration) *Recorder {
	r.pause = d
	return r
}

// WithMaxDaysPerRun overrides DefaultMaxDaysPerRun (tests).
func (r *Recorder) WithMaxDaysPerRun(n int) *Recorder {
	r.maxDays = n
	return r
}

// Run records up to the per-run day limit. Today's JST day is never labeled
// (it is still being collected), so a day is only ever labeled complete. The
// first failure stops the run and is returned; the days completed before it
// stay marked.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	var res Result
	today := r.now().In(jst).Format(time.DateOnly)
	days, err := r.store.PendingDays(ctx, today, r.maxDays)
	if err != nil {
		return res, fmt.Errorf("researchlabel: list pending days: %w", err)
	}
	if len(days) == 0 {
		return res, nil
	}
	costs, err := r.costs(ctx)
	if err != nil {
		return res, err
	}
	for _, day := range days {
		snapshots, rows, err := r.recordDay(ctx, day, costs)
		res.Snapshots += snapshots
		res.Rows += rows
		if err != nil {
			return res, err
		}
		res.Days = append(res.Days, day)
	}
	return res, nil
}

// recordDay labels every instrument of day, then marks the day done.
func (r *Recorder) recordDay(ctx context.Context, day string, costs Costs) (snapshots, rows int, err error) {
	ids, err := r.store.InstrumentIDs(ctx, day)
	if err != nil {
		return 0, 0, fmt.Errorf("researchlabel: list instruments of %s: %w", day, err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return snapshots, rows, err
		}
		bars, err := r.store.Bars(ctx, day, id)
		if err != nil {
			return snapshots, rows, fmt.Errorf("researchlabel: read bars of instrument %d on %s: %w", id, day, err)
		}
		snapshots += len(bars)
		n, err := r.store.Insert(ctx, LabelBars(bars, costs), r.now())
		rows += n
		if err != nil {
			return snapshots, rows, fmt.Errorf("researchlabel: record instrument %d on %s: %w", id, day, err)
		}
		if r.pause > 0 {
			select {
			case <-ctx.Done():
				return snapshots, rows, ctx.Err()
			case <-time.After(r.pause):
			}
		}
	}
	if err := r.store.MarkDone(ctx, day, snapshots, r.now()); err != nil {
		return snapshots, rows, fmt.Errorf("researchlabel: mark %s done: %w", day, err)
	}
	slog.Info("researchlabel: day recorded", "day", day, "instruments", len(ids), "snapshots", snapshots, "new_rows", rows)
	return snapshots, rows, nil
}

package researchlabel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
)

// DefaultMaxDaysPerRun bounds how many JST days one Run records, oldest first:
// one. The first run over the whole retained history (90 days) is thereby 90
// short chunks, each a Run that reports Result.More, instead of one long call:
// the maintenance runner (scheduler/maintenance) holds its lock for the length of
// a call, and the database backup and the retention purge wait on that lock.
const DefaultMaxDaysPerRun = 1

// PurgeBatchRows is how many rows one DELETE removes, and DefaultMaxPurgeBatches
// how many such deletes one Run makes before it reports More and leaves the rest
// to the next Run (a chunk of at most 10 000 rows, bounded like a day's labeling).
const (
	PurgeBatchRows         = 1000
	DefaultMaxPurgeBatches = 10
)

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
	// PendingDays returns up to limit days from the day from ("" for no lower
	// bound) and before the day before, oldest first, that have market_snapshots
	// rows and no completion mark.
	PendingDays(ctx context.Context, from, before string, limit int) ([]string, error)
	// InstrumentIDs are the instruments with a bar on day.
	InstrumentIDs(ctx context.Context, day string) ([]int64, error)
	// Bars are one instrument's bars of day, ascending.
	Bars(ctx context.Context, day string, instrumentID int64) ([]domain.Snapshot, error)
	// Insert adds the rows (rows already recorded are left as they are) and
	// returns how many were new. One short transaction.
	Insert(ctx context.Context, rows []rlabel.Label, createdAt time.Time) (int, error)
	// MarkDone records that every instrument of day was labeled.
	MarkDone(ctx context.Context, day string, snapshots int, completedAt time.Time) error
	// PurgeBefore deletes up to limit rows of research_horizon_labels whose day
	// is before the given day and returns how many. One short transaction.
	PurgeBefore(ctx context.Context, day string, limit int) (int, error)
}

// RetentionSource returns how many JST days of rows to keep
// (researchlabelsettings.Load).
type RetentionSource func(ctx context.Context) (days int, err error)

// CostSource returns the costs the label assumes at record time
// (CostsFromSettings over the Settings).
type CostSource func(ctx context.Context) (Costs, error)

// Result is what one Run recorded.
type Result struct {
	// Days are the JST days completed in this run.
	Days []string
	// Purged is how many old rows of research_horizon_labels this run deleted.
	Purged int
	// More is true when this run stopped at its bound with work left (days to
	// record, rows to purge): call Run again. Every run with More set made
	// progress.
	More bool
	// Snapshots are the bars read; Rows the new research_horizon_labels rows.
	Snapshots, Rows int
}

// Recorder writes the research labels of every JST day that is complete (strictly
// before today) and not yet marked done. It is resumable and idempotent: a day
// is marked only after all its instruments are labeled, so a run that stops
// midway is redone from the start of that day, and rows already written are
// kept by UNIQUE (snapshot_id, horizon).
type Recorder struct {
	store     Store
	costs     CostSource
	retention RetentionSource // nil: keep every row, record every pending day
	now       func() time.Time
	maxDays   int
	maxPurge  int
	// pause is how long recordDay waits after each instrument's insert so a writer
	// waiting on busy_timeout can take the write lock (WAL has no fair queue;
	// the same reasoning as retention's batch pause, issue #533). 0 = no pause.
	pause time.Duration
}

// NewRecorder returns a Recorder over store, with costs read at the start of
// every Run.
func NewRecorder(store Store, costs CostSource) *Recorder {
	return &Recorder{store: store, costs: costs, now: time.Now, maxDays: DefaultMaxDaysPerRun, maxPurge: DefaultMaxPurgeBatches}
}

// WithRetention sets the retention in JST days, read at the start of every Run:
// the rows of days before today - days are deleted from research_horizon_labels,
// and such days are not recorded (they would be deleted again at once). Without
// it nothing is deleted.
func (r *Recorder) WithRetention(days RetentionSource) *Recorder {
	r.retention = days
	return r
}

// WithMaxPurgeBatches overrides DefaultMaxPurgeBatches (tests).
func (r *Recorder) WithMaxPurgeBatches(n int) *Recorder {
	r.maxPurge = n
	return r
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

// Run does one bounded chunk of work: first the purge of rows older than the
// retention (at most maxPurge batches), then the recording of up to maxDays
// complete JST days, oldest first. Today's JST day is never labeled (it is still
// being collected), so a day is only ever labeled complete, and a day older than
// the retention is not labeled at all. Result.More says the chunk stopped at its
// bound with work left; the caller calls Run again (the maintenance runner does,
// between chunks and with its lock released). The first failure stops the run and
// is returned; the days completed before it stay marked.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	var res Result
	today := r.now().In(jst).Format(time.DateOnly)
	from := ""
	if r.retention != nil {
		keep, err := r.retention(ctx)
		if err != nil {
			return res, fmt.Errorf("researchlabel: read retention: %w", err)
		}
		cutoff, err := time.ParseInLocation(time.DateOnly, today, jst)
		if err != nil {
			return res, err
		}
		from = cutoff.AddDate(0, 0, -keep).Format(time.DateOnly)
		purged, more, err := r.purge(ctx, from)
		res.Purged = purged
		if err != nil {
			return res, err
		}
		if more {
			res.More = true
			return res, nil
		}
	}
	days, err := r.store.PendingDays(ctx, from, today, r.maxDays+1)
	if err != nil {
		return res, fmt.Errorf("researchlabel: list pending days: %w", err)
	}
	if len(days) > r.maxDays {
		days, res.More = days[:r.maxDays], true
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

// purge deletes the rows of days before `before` in batches of PurgeBatchRows,
// pausing between batches, at most maxPurge of them; more is true when the last
// batch was full and the bound was reached.
func (r *Recorder) purge(ctx context.Context, before string) (deleted int, more bool, err error) {
	for batch := 0; batch < r.maxPurge; batch++ {
		n, err := r.store.PurgeBefore(ctx, before, PurgeBatchRows)
		deleted += n
		if err != nil {
			return deleted, false, fmt.Errorf("researchlabel: purge rows before %s: %w", before, err)
		}
		if n < PurgeBatchRows {
			return deleted, false, nil
		}
		if batch == r.maxPurge-1 {
			return deleted, true, nil
		}
		if err := r.wait(ctx); err != nil {
			return deleted, false, err
		}
	}
	return deleted, false, nil
}

// wait sleeps the batch pause (none when 0), or returns the context's error.
func (r *Recorder) wait(ctx context.Context) error {
	if r.pause <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(r.pause):
		return nil
	}
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
		if err := r.wait(ctx); err != nil {
			return snapshots, rows, err
		}
	}
	if err := r.store.MarkDone(ctx, day, snapshots, r.now()); err != nil {
		return snapshots, rows, fmt.Errorf("researchlabel: mark %s done: %w", day, err)
	}
	slog.Info("researchlabel: day recorded", "day", day, "instruments", len(ids), "snapshots", snapshots, "new_rows", rows)
	return snapshots, rows, nil
}

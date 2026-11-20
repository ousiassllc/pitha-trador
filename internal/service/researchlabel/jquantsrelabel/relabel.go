// Package jquantsrelabel re-labels one JST day of the research horizon labels
// (issue #1074) with minute bars from J-Quants (issue #1093), and removes those
// rows again when the subscription is cancelled.
//
// market_snapshots is the record of what the application saw; where a collector
// outage left a hole, the 30-minute / 60-minute / close windows that needed a bar
// in the hole got no label row (the recorder never shortens a window). J-Quants
// minute bars fill such a hole as EXIT bars only: they carry no bid or ask, so
// they can never be an entry. The entries stay the market_snapshots bars. A
// day with no snapshots at all therefore cannot be re-labeled: there is no entry.
//
// Only research_horizon_labels (and the day's sums) are written, never a live
// label, a decision or a Jev table. Rows the recorder already wrote are left
// exactly as they are (INSERT OR IGNORE on (snapshot_id, horizon)); the rows this
// package adds carry exit_source = 'jquants' so Purge can find and delete them.
package jquantsrelabel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	jq "github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/researchlabel"
)

// ErrDayNotRecorded means the recorder has not marked the day done yet: there is
// nothing to complete, and the recorder will label the day itself.
var ErrDayNotRecorded = errors.New("jquantsrelabel: the day's research labels are not recorded yet")

// PurgeBatchRows is how many rows one purge transaction deletes.
const PurgeBatchRows = 1000

// Store is the persistence Relabeler needs (repository/researchlabel/jquantslabel).
type Store interface {
	RecordedSnapshots(ctx context.Context, day string) (snapshots int, marked bool, err error)
	InstrumentIDs(ctx context.Context, day string) ([]int64, error)
	Bars(ctx context.Context, day string, instrumentID int64) ([]domain.Snapshot, error)
	InsertJQuants(ctx context.Context, rows []rlabel.Label, createdAt time.Time) (int, error)
	// MarkDone rewrites the day's mark and sums from the rows present.
	MarkDone(ctx context.Context, day string, snapshots int, completedAt time.Time) error
	DeleteJQuants(ctx context.Context, limit int) (deleted int, days []string, err error)
}

// Minutes are the stored J-Quants minute bars (repository/jquants).
type Minutes interface {
	MinuteBars(ctx context.Context, day, symbol string) ([]jq.MinuteBar, error)
}

// Relabeler fills the research labels of a day with J-Quants exits.
type Relabeler struct {
	store   Store
	minutes Minutes
	costs   researchlabel.CostSource
	now     func() time.Time
}

// New returns a Relabeler; costs are the same source the recorder uses, read at
// every Relabel.
func New(store Store, minutes Minutes, costs researchlabel.CostSource) *Relabeler {
	return &Relabeler{store: store, minutes: minutes, costs: costs, now: time.Now}
}

// WithClock overrides time.Now (tests).
func (r *Relabeler) WithClock(now func() time.Time) *Relabeler {
	r.now = now
	return r
}

// Result is what one Relabel added.
type Result struct {
	// Instruments are the instruments with bars on the day; Filled those that got
	// at least one J-Quants exit bar into a hole.
	Instruments, Filled int
	// NewRows are the new research_horizon_labels rows.
	NewRows int
}

// Relabel labels the JST day again with the stored minute bars used as exits
// where market_snapshots has no bar in the minute, and refreshes the day's sums
// when it added rows. It is idempotent: a second call adds nothing.
func (r *Relabeler) Relabel(ctx context.Context, day string) (Result, error) {
	var res Result
	snapshots, marked, err := r.store.RecordedSnapshots(ctx, day)
	if err != nil {
		return res, err
	}
	if !marked {
		return res, ErrDayNotRecorded
	}
	ids, err := r.store.InstrumentIDs(ctx, day)
	if err != nil {
		return res, fmt.Errorf("jquantsrelabel: list instruments of %s: %w", day, err)
	}
	costs, err := r.costs(ctx)
	if err != nil {
		return res, err
	}
	res.Instruments = len(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		bars, err := r.store.Bars(ctx, day, id)
		if err != nil {
			return res, fmt.Errorf("jquantsrelabel: read bars of instrument %d on %s: %w", id, day, err)
		}
		if len(bars) == 0 {
			continue
		}
		minutes, err := r.minutes.MinuteBars(ctx, day, bars[0].Symbol)
		if err != nil {
			return res, err
		}
		merged, filled := MergeExits(bars, minutes)
		if len(filled) == 0 {
			continue
		}
		var rows []rlabel.Label
		for _, l := range researchlabel.LabelBars(merged, costs) {
			if filled[l.ExitAt] {
				rows = append(rows, l)
			}
		}
		n, err := r.store.InsertJQuants(ctx, rows, r.now())
		res.NewRows += n
		if err != nil {
			return res, fmt.Errorf("jquantsrelabel: record instrument %d on %s: %w", id, day, err)
		}
		res.Filled++
	}
	if res.NewRows > 0 {
		if err := r.store.MarkDone(ctx, day, snapshots, r.now()); err != nil {
			return res, fmt.Errorf("jquantsrelabel: refresh the sums of %s: %w", day, err)
		}
	}
	slog.Info("jquantsrelabel: day re-labeled", "day", day, "instruments", res.Instruments, "filled", res.Filled, "new_rows", res.NewRows)
	return res, nil
}

// PurgeDerived deletes every research label row whose exit came from J-Quants,
// in batches of PurgeBatchRows, and refreshes the sums of the days it removed rows
// from, so no figure computed from J-Quants data stays behind. It returns how many
// rows it deleted.
func (r *Relabeler) PurgeDerived(ctx context.Context) (int, error) {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, days, err := r.store.DeleteJQuants(ctx, PurgeBatchRows)
		total += n
		if err != nil {
			return total, err
		}
		for _, day := range days {
			snapshots, marked, err := r.store.RecordedSnapshots(ctx, day)
			if err != nil {
				return total, err
			}
			if !marked {
				continue
			}
			if err := r.store.MarkDone(ctx, day, snapshots, r.now()); err != nil {
				return total, fmt.Errorf("jquantsrelabel: refresh the sums of %s: %w", day, err)
			}
		}
		if n < PurgeBatchRows {
			return total, nil
		}
	}
}

// MergeExits adds, to the market_snapshots bars (ascending), one exit-only bar
// per J-Quants minute that holds no snapshot bar, and returns the merged series
// (ascending) and the set of the added bars' times. The added bar is the
// minute's close at the LAST second of the minute (clamped to the TSE close), the
// latest moment the close can be known by, so an exit is never dated before the
// price it uses. It has no bid or ask: LabelBars never enters on it.
func MergeExits(bars []domain.Snapshot, minutes []jq.MinuteBar) (merged []domain.Snapshot, added map[time.Time]bool) {
	merged = append([]domain.Snapshot(nil), bars...)
	if len(bars) == 0 {
		return merged, nil
	}
	taken := map[time.Time]bool{}
	for _, b := range bars {
		taken[b.Timestamp.Truncate(time.Minute)] = true
	}
	added = map[time.Time]bool{}
	for _, m := range minutes {
		start := m.At.Truncate(time.Minute)
		if taken[start] {
			continue
		}
		at := start.Add(time.Minute - time.Second)
		if closeAt, ok := marketcalendar.TSE.CloseAt(start); ok && at.After(closeAt) {
			at = closeAt
		}
		taken[start] = true
		added[at] = true
		merged = append(merged, domain.Snapshot{InstrumentID: bars[0].InstrumentID, Symbol: bars[0].Symbol, Timestamp: at, Price: m.Close})
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Timestamp.Before(merged[j].Timestamp) })
	return merged, added
}

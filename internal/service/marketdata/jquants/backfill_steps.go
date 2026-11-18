package jquants

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// undeliveredDays is how many days back an empty minute response still counts as
// "not delivered yet" rather than "no trades".
const undeliveredDays = 3

// fetchSymbols reads the master once per JST day.
func (r *run) fetchSymbols(ctx context.Context) error {
	covered, err := r.store.Covered(ctx, jquants.DatasetSymbols, r.today)
	if err != nil {
		return err
	}
	if covered[""] {
		return nil
	}
	if err := r.call(); err != nil {
		return err
	}
	rows, err := r.reader.Symbols(ctx, r.today)
	if err != nil {
		return err
	}
	at := r.now()
	n, err := r.store.UpsertSymbols(ctx, rows, at)
	if err != nil {
		return err
	}
	if err := r.store.MarkCovered(ctx, jquants.DatasetSymbols, r.today, "", n, at); err != nil {
		return err
	}
	return r.store.RecordSuccess(ctx, r.today, jquants.DatasetSymbols, n, at)
}

// fetchDay fetches what the plan allows for one trading day: the daily bars of
// every issue, then the minute bars of the day's symbols, then the re-labeling.
func (r *run) fetchDay(ctx context.Context, day string) error {
	if r.s.PlanDaily {
		if err := r.fetchDaily(ctx, day); err != nil {
			return err
		}
	}
	if r.s.PlanMinute {
		return r.fetchMinutes(ctx, day)
	}
	return nil
}

func (r *run) fetchDaily(ctx context.Context, day string) error {
	covered, err := r.store.Covered(ctx, jquants.DatasetDaily, day)
	if err != nil || covered[""] {
		return err
	}
	if err := r.call(); err != nil {
		return err
	}
	rows, err := r.reader.DailyBars(ctx, day)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		// A trading day with no rows is not delivered yet: leave it unmarked.
		return nil
	}
	at := r.now()
	n, err := r.store.UpsertDailyBars(ctx, rows, at)
	if err != nil {
		return err
	}
	if err := r.markCovered(ctx, jquants.DatasetDaily, day, "", n, at); err != nil {
		return err
	}
	r.res.DailyDays = append(r.res.DailyDays, day)
	r.res.DailyRows += n
	return r.store.RecordSuccess(ctx, day, jquants.DatasetDaily, n, at)
}

// fetchMinutes fetches the minute bars of each symbol of the day that has no
// mark, then (once every symbol is marked) re-labels the day. The minute bars
// reach back two years only; an older day is not asked for.
func (r *run) fetchMinutes(ctx context.Context, day string) error {
	today, err := time.ParseInLocation(time.DateOnly, r.today, marketcalendar.JST)
	if err != nil {
		return err
	}
	if day < today.AddDate(0, 0, -jquantssettings.MaxMinuteDays).Format(time.DateOnly) {
		return nil
	}
	symbols, err := r.minuteSymbols(ctx, day)
	if err != nil || len(symbols) == 0 {
		return err
	}
	covered, err := r.store.Covered(ctx, jquants.DatasetMinute, day)
	if err != nil {
		return err
	}
	fetched := 0
	for _, symbol := range symbols {
		code := jquants.CodeOf(symbol)
		if covered[code] {
			continue
		}
		if err := r.call(); err != nil {
			return err
		}
		rows, err := r.reader.MinuteBars(ctx, day, code)
		if err != nil {
			return err
		}
		if len(rows) == 0 && day >= today.AddDate(0, 0, -undeliveredDays).Format(time.DateOnly) {
			// The minute bars are delivered the next day: for a recent day, no rows
			// means "not delivered yet". Leave it unmarked and try on a later run.
			return nil
		}
		at := r.now()
		n, err := r.store.UpsertMinuteBars(ctx, rows, at)
		if err != nil {
			return err
		}
		// For an older day, an issue with no rows had no execution: complete all the same.
		if err := r.markCovered(ctx, jquants.DatasetMinute, day, code, n, at); err != nil {
			return err
		}
		if err := r.store.RecordSuccess(ctx, day, jquants.DatasetMinute, n, at); err != nil {
			return err
		}
		r.res.MinuteRows += n
		fetched++
	}
	if fetched > 0 {
		r.res.MinuteDays = append(r.res.MinuteDays, day)
	}
	return r.relabelDay(ctx, day)
}

// minuteSymbols are the symbols to fetch: the request's, else those the
// application recorded on the day (sorted, without duplicates).
func (r *run) minuteSymbols(ctx context.Context, day string) ([]string, error) {
	if len(r.req.Symbols) > 0 {
		out := slices.Clone(r.req.Symbols)
		slices.Sort(out)
		return slices.Compact(out), nil
	}
	return r.symbols.SnapshotSymbols(ctx, day)
}

// relabelDay re-labels the day once, after all its minute bars are stored.
func (r *run) relabelDay(ctx context.Context, day string) error {
	if r.relabel == nil {
		return nil
	}
	done, err := r.store.Covered(ctx, jquants.DatasetRelabel, day)
	if err != nil || done[""] {
		return err
	}
	rows, ready, err := r.relabel(ctx, day)
	if err != nil {
		return fmt.Errorf("jquants: re-label %s: %w", day, err)
	}
	if !ready {
		return nil
	}
	if err := r.markCovered(ctx, jquants.DatasetRelabel, day, "", rows, r.now()); err != nil {
		return err
	}
	r.res.Relabeled = append(r.res.Relabeled, day)
	r.res.RelabelRows += rows
	return nil
}

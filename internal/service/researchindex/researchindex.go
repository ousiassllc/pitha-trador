// Package researchindex records the research-only index series (issue #1084):
// for each configured index code (日経平均 = 101, TOPIX = 151 by default) and each
// completed trading day of a short lookback window, it copies the 1-minute values
// that the 立花 quote recording (issue #1083) already
// stored into research_index_minutes, and writes the day's marker
// (internal/repository/researchindex). When 立花 stored no value of the index that
// day, the marker says "unavailable": the day was checked and the index was not
// obtained (never silently skipped, never a made-up value).
//
// It makes no request to anything: it reads the local table. So it uses none of
// the kabu registration slots and never changes which symbols kabu or 立花
// watch; to have values, the index codes have to be among the symbols the 立花
// recording subscribes to (research.tachibana_quotes.extra_symbols). The record
// is never a trading input. This package must not import a broker, kabu or
// tachibana client, nor Feature Engine, Policy, Risk, order or Jev code
// (repository/researchindex/isolation_test.go pins that).
package researchindex

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// DefaultLookbackDays is how many completed trading days the Recorder looks at: a
// day in the window with no marker is checked, oldest first. A day older than the
// window that was never checked stays a gap.
const DefaultLookbackDays = 5

// CompleteAfter is the JST time of day after which a trading day counts as
// complete: five minutes after the 15:30 close, so the last minute (15:29, taken
// at its 5th second) is stored.
const CompleteAfter = 15*time.Hour + 35*time.Minute

// searchDays bounds Window's scan back over calendar days (a trading day is
// never more than a few days apart, and the year-end break is under two weeks).
const searchDays = 60

// Source reads the stored 立花 values of an index (researchindex.Repository.Samples).
type Source interface {
	Samples(ctx context.Context, symbol, day string) ([]rindex.Minute, error)
}

// Store is the persistence the Recorder needs (internal/repository/researchindex).
type Store interface {
	// MarkedDays are the days of symbol in [from, to] that have a marker.
	MarkedDays(ctx context.Context, symbol, from, to string) (map[string]bool, error)
	// RecordDay writes the minutes of a day with its marker in one transaction.
	RecordDay(ctx context.Context, symbol, day string, minutes []rindex.Minute, at time.Time) (rindex.Day, error)
}

// SymbolSource returns the index codes to record (researchindexsettings.Load).
type SymbolSource func(ctx context.Context) ([]string, error)

// Result is what one Run did.
type Result struct {
	// Recorded and Unavailable count the (code, day) units marked 'recorded' and
	// 'unavailable'; Minutes the values written.
	Recorded, Unavailable, Minutes int
}

// Recorder checks the unmarked (code, trading day) units of the window.
type Recorder struct {
	source   Source
	store    Store
	symbols  SymbolSource
	now      func() time.Time
	lookback int
}

// NewRecorder returns a Recorder over source and store, with the codes read at the
// start of every Run.
func NewRecorder(source Source, store Store, symbols SymbolSource) *Recorder {
	return &Recorder{source: source, store: store, symbols: symbols, now: time.Now, lookback: DefaultLookbackDays}
}

// WithClock overrides time.Now (tests).
func (r *Recorder) WithClock(now func() time.Time) *Recorder {
	r.now = now
	return r
}

// WithLookbackDays overrides DefaultLookbackDays (tests).
func (r *Recorder) WithLookbackDays(n int) *Recorder {
	r.lookback = n
	return r
}

// Window returns the completed trading days the Recorder considers at now, oldest
// first: the last lookbackDays trading days (TSE calendar) up to today's, today's
// only once CompleteAfter has passed.
func Window(now time.Time, lookbackDays int) []string {
	n := now.In(rindex.JST)
	day := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, rindex.JST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < CompleteAfter {
		day = day.AddDate(0, 0, -1)
	}
	var days []string
	for i := 0; i < searchDays && len(days) < lookbackDays; i++ {
		if marketcalendar.TSE.IsTradingDay(day) {
			days = append(days, day.Format(time.DateOnly))
		}
		day = day.AddDate(0, 0, -1)
	}
	slices.Reverse(days)
	return days
}

// SessionMinutes returns the start of every session minute of the trading day
// (09:00-11:30 and 12:30-15:30 JST, 330 minutes), oldest first; nil when day is
// not a TSE trading day or not a date.
func SessionMinutes(day string) []time.Time {
	d, err := rindex.DayStart(day)
	if err != nil {
		return nil
	}
	var out []time.Time
	for _, s := range marketcalendar.TSE.Sessions(d) {
		for t := s.Open; t.Before(s.Close); t = t.Add(time.Minute) {
			out = append(out, t.UTC())
		}
	}
	return out
}

// Run checks every unmarked (code, day) unit of the window, oldest day first,
// codes in the configured order. A unit gets its marker only after its values are
// written, so a failure leaves the unit unmarked and the next Run retries it.
// With nothing pending it does nothing. The first failure is returned.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	symbols, err := r.symbols(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("researchindex: read codes: %w", err)
	}
	symbols = slices.Compact(slices.Clone(symbols))
	days := Window(r.now(), r.lookback)
	var res Result
	if len(days) == 0 {
		return res, nil
	}
	marked := make(map[string]map[string]bool, len(symbols))
	for _, s := range symbols {
		if marked[s], err = r.store.MarkedDays(ctx, s, days[0], days[len(days)-1]); err != nil {
			return res, err
		}
	}
	for _, day := range days {
		for _, s := range symbols {
			if marked[s][day] {
				continue
			}
			d, err := r.checkDay(ctx, s, day)
			if err != nil {
				return res, err
			}
			if d.Status == rindex.StatusRecorded {
				res.Recorded++
			} else {
				res.Unavailable++
			}
			res.Minutes += d.MinuteCount
		}
	}
	return res, nil
}

// checkDay copies the stored values of (symbol, day) that lie in a session minute
// (the 立花 recorder writes nothing else) and marks the day.
func (r *Recorder) checkDay(ctx context.Context, symbol, day string) (rindex.Day, error) {
	samples, err := r.source.Samples(ctx, symbol, day)
	if err != nil {
		return rindex.Day{}, err
	}
	minutes := make([]rindex.Minute, 0, len(samples))
	for _, m := range samples {
		if marketcalendar.TSE.IsOpen(m.Time) {
			minutes = append(minutes, m)
		}
	}
	d, err := r.store.RecordDay(ctx, symbol, day, minutes, r.now())
	if err != nil {
		return rindex.Day{}, err
	}
	slog.Info("researchindex: day checked", "symbol", symbol, "day", day, "status", d.Status, "minutes", d.MinuteCount)
	return d, nil
}

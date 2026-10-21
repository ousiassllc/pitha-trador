package training

import (
	"errors"
	"fmt"
	"sort"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// ErrInsufficientData is wrapped by the errors SplitWalkForward and the
// Trainer return when there is not enough (or not the right kind of) labeled
// history to learn a model from. It is a normal early-days condition, not a
// failure: the caller keeps whatever model it already has.
var ErrInsufficientData = errors.New("metalabel: insufficient training data")

// Row is one labeled primary signal prepared for the walk-forward: when it was
// decided, when its label is fully known, the model input it had and its
// cost-adjusted outcome.
type Row struct {
	JevDecisionID int64
	// JevModelID is the Jev model that answered ("" when unknown).
	JevModelID string
	// Symbol is the decision's instrument, a bootstrap cluster key (#780).
	Symbol string
	// DecidedAt is the decision's time and LabelEnd the end of its label
	// window (DecidedAt + the triple-barrier timeout). The label of a row
	// depends on prices up to LabelEnd, so a model trained on it has seen the
	// market until then.
	DecidedAt, LabelEnd time.Time
	Direction           string
	Features            metalabel.Features
	// Vector is Features.Vector(Direction), the model input.
	Vector       []float64
	NetReturnBps float64
}

// Sample returns r as a training sample.
func (r Row) Sample() metalabel.Sample {
	return metalabel.Sample{Vector: r.Vector, NetReturnBps: r.NetReturnBps}
}

// WalkForwardConfig configures SplitWalkForward.
//
// The OOS window is sized by what the adoption bar needs, not by a fixed
// length (issue #862): with about a hundred usable rows a trading day a fixed
// 7-day window holds about 5 trading days, 600 rows, so the 1084 rows the
// earlier bar asked for could never be reached. The window reaches back until
// it holds OOSMinRows rows and OOSMinDays trading days, but not further than
// OOSMaxWindow and never before the LLM cutoff; older rows only train.
type WalkForwardConfig struct {
	// OOSMinRows is the fewest rows the OOS window reaches back to hold, > 0.
	// The Trainer raises it to the bar's MinOOSSamples, so the window is never
	// narrower than what the bar asks for.
	OOSMinRows int
	// OOSMinDays is the fewest JST trading days the window reaches back to
	// span (whole days), > 0; the Trainer raises it to the bar's MinOOSDays.
	OOSMinDays int
	// OOSMaxWindow is how far back from now the window may reach, > 0.
	OOSMaxWindow time.Duration
	// CalibrationFraction is the share (by time, newest) of the pre-OOS rows
	// the calibrator is fitted on, in (0, 1); the older rest fits the weights.
	CalibrationFraction float64
	// Embargo is the gap kept between the end of one segment's labels and the
	// start of the next, >= 0. Together with the purge (a row whose label
	// window reaches into the next segment is dropped) it stops serially
	// correlated neighbours from leaking the outcome across a boundary.
	Embargo time.Duration
	// MinFit, MinCalibration are the smallest fit and calibration sets a model
	// is learned from, > 0. A smaller OOS set is not an error here: the model
	// is learned and then rejected by its (undefined or noisy) metrics.
	MinFit, MinCalibration int
}

// DefaultWalkForwardConfig is the proposed split (issue #862; the numbers await
// approval, see FR-META-7): an OOS window that holds at least the default bar's
// samples and days (DefaultAcceptanceConfig) within DefaultOOSMaxWindow, the
// newest quarter of the rest for calibration, a 30-minute embargo (half the
// longest triple-barrier timeout, 60 minutes) and at least 100 fit and 30
// calibration rows (the logistic regression has 8 features and a bias: 9
// parameters, so 100 rows is above the rule of thumb of 10 rows per parameter).
func DefaultWalkForwardConfig() WalkForwardConfig {
	bar := DefaultAcceptanceConfig()
	return WalkForwardConfig{
		OOSMinRows: bar.MinOOSSamples, OOSMinDays: bar.MinOOSDays, OOSMaxWindow: DefaultOOSMaxWindow,
		CalibrationFraction: 0.25, Embargo: 30 * time.Minute,
		MinFit: 100, MinCalibration: 30,
	}
}

// Validate reports a configuration SplitWalkForward would not accept.
func (c WalkForwardConfig) Validate() error {
	switch {
	case c.OOSMinRows <= 0 || c.OOSMinDays <= 0:
		return fmt.Errorf("metalabel: OOS minimum rows/days must be positive (%d/%d)", c.OOSMinRows, c.OOSMinDays)
	case c.OOSMaxWindow <= 0:
		return fmt.Errorf("metalabel: OOS max window %v must be positive", c.OOSMaxWindow)
	case !(c.CalibrationFraction > 0 && c.CalibrationFraction < 1):
		return fmt.Errorf("metalabel: calibration fraction %v must be in (0, 1)", c.CalibrationFraction)
	case c.Embargo < 0:
		return fmt.Errorf("metalabel: embargo %v must not be negative", c.Embargo)
	case c.MinFit <= 0 || c.MinCalibration <= 0:
		return fmt.Errorf("metalabel: minimum fit/calibration sizes must be positive (%d/%d)", c.MinFit, c.MinCalibration)
	}
	return nil
}

// oosStart returns where the OOS window over the time-ordered rows starts: as
// far back as needed to hold OOSMinRows rows and OOSMinDays trading days (the
// newest days whole, the bar resamples whole days; the rows may open the oldest
// day part way), but not before max(now-OOSMaxWindow, llmCutoff), and not after now.
func (c WalkForwardConfig) oosStart(sorted []Row, now, llmCutoff time.Time) time.Time {
	floor := now.Add(-c.OOSMaxWindow)
	if llmCutoff.After(floor) {
		floor = llmCutoff
	}
	if floor.After(now) {
		return now
	}
	first := sort.Search(len(sorted), func(i int) bool { return !sorted[i].DecidedAt.Before(floor) })
	reachable := sorted[first:]
	i := min(len(reachable)-c.OOSMinRows, firstRowOfNewestDays(reachable, c.OOSMinDays))
	if i <= 0 {
		return floor
	}
	return reachable[i].DecidedAt
}

// firstRowOfNewestDays is the index of the first row of the days-th newest
// JST day of the time-ordered rows (0 when they span fewer days).
func firstRowOfNewestDays(rows []Row, days int) int {
	seen, prev := 0, ""
	for j := len(rows) - 1; j >= 0; j-- {
		if d := dayOf(rows[j]); d != prev {
			seen, prev = seen+1, d
			if seen > days {
				return j + 1
			}
		}
	}
	return 0
}

// Split is one walk-forward fold: fit (weights) < calibrate < OOS in time,
// separated by purge and embargo.
type Split struct {
	Fit, Calibrate, OOS []Row
	// TrainStart is the first fit row's decision time and TrainEnd the latest
	// label end among the fit and calibration rows: the model has seen the
	// market up to TrainEnd and not beyond. OOSStart >= TrainEnd +
	// the embargo always holds.
	TrainStart, TrainEnd time.Time
	// OOSStart and OOSEnd bound the out-of-sample window: [OOSStart, OOSEnd).
	// OOSStart is never before the LLM cutoff.
	OOSStart, OOSEnd time.Time
	// LLMCutoff is the cutoff OOSStart was held to.
	LLMCutoff time.Time
	// Purged is the number of rows dropped by the purge/embargo (their label
	// window reached into, or was too close to, the next segment).
	Purged int
}

// SplitWalkForward cuts rows (any order) into a fit, a calibration and an
// out-of-sample set as of now, for a model that scores Jev features:
//
//   - The OOS window is [oosStart, now) with oosStart as far back as the config
//     asks (see WalkForwardConfig) and never before max(now-OOSMaxWindow,
//     llmCutoff): Jev's answers are only evaluated on decisions on or after the
//     LLM's training-data cutoff, before which the LLM may already know how the
//     day went (arXiv:2512.23847 look-ahead bias). With llmCutoff at or after
//     now there is no OOS window and OOS is empty.
//   - Rows before OOSStart are split by time into fit (older) and calibration
//     (newest CalibrationFraction).
//   - A row is purged when its label window ends less than Embargo before the
//     start of the segment that follows it.
//
// It fails with ErrInsufficientData when the fit or calibration set is smaller
// than the configured minimum; the error carries the row counts of both sides
// and what each needs.
func SplitWalkForward(rows []Row, now, llmCutoff time.Time, cfg WalkForwardConfig) (Split, error) {
	if err := cfg.Validate(); err != nil {
		return Split{}, err
	}
	sorted := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r.DecidedAt.Before(now) && !r.LabelEnd.After(now) {
			sorted = append(sorted, r)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DecidedAt.Before(sorted[j].DecidedAt) })

	oosStart := cfg.oosStart(sorted, now, llmCutoff)
	s := Split{OOSStart: oosStart, OOSEnd: now, LLMCutoff: llmCutoff}

	var pre []Row
	for _, r := range sorted {
		if r.DecidedAt.Before(oosStart) {
			pre = append(pre, r)
		} else {
			s.OOS = append(s.OOS, r)
		}
	}
	pre, purged := purge(pre, oosStart, cfg.Embargo)
	s.Purged += purged
	if len(pre) < cfg.MinFit+cfg.MinCalibration {
		return Split{}, &InsufficientDataError{
			Code: repo.SkipTooFewRowsBeforeOOS, Usable: len(pre), Required: cfg.MinFit + cfg.MinCalibration,
			OOSRows: len(s.OOS), RequiredOOSRows: cfg.OOSMinRows,
			Detail: fmt.Sprintf("%d usable rows before the OOS window, need at least %d for fit and calibration", len(pre), cfg.MinFit+cfg.MinCalibration),
		}
	}

	nCal := max(cfg.MinCalibration, int(float64(len(pre))*cfg.CalibrationFraction+0.5))
	calStart := pre[len(pre)-nCal].DecidedAt
	var before []Row
	for _, r := range pre {
		if r.DecidedAt.Before(calStart) {
			before = append(before, r)
		} else {
			s.Calibrate = append(s.Calibrate, r)
		}
	}
	s.Fit, purged = purge(before, calStart, cfg.Embargo)
	s.Purged += purged
	if len(s.Fit) < cfg.MinFit || len(s.Calibrate) < cfg.MinCalibration {
		// Each set counts at most up to its own minimum, so the total stays
		// below the requirement it is shown against ("必要130件中 x件").
		return Split{}, &InsufficientDataError{
			Code: repo.SkipTooFewRowsAfterPurge, Usable: min(len(s.Fit), cfg.MinFit) + min(len(s.Calibrate), cfg.MinCalibration),
			Required: cfg.MinFit + cfg.MinCalibration, OOSRows: len(s.OOS), RequiredOOSRows: cfg.OOSMinRows,
			Detail: fmt.Sprintf("%d fit and %d calibration rows after purging, need %d and %d", len(s.Fit), len(s.Calibrate), cfg.MinFit, cfg.MinCalibration),
		}
	}

	s.TrainStart = s.Fit[0].DecidedAt
	for _, r := range append(append([]Row(nil), s.Fit...), s.Calibrate...) {
		if r.LabelEnd.After(s.TrainEnd) {
			s.TrainEnd = r.LabelEnd
		}
	}
	return s, nil
}

// purge drops the rows whose label window ends within embargo of next (the
// start of the following segment) or after it, returning the survivors and the
// number dropped.
func purge(rows []Row, next time.Time, embargo time.Duration) ([]Row, int) {
	kept := rows[:0:0]
	for _, r := range rows {
		if r.LabelEnd.Add(embargo).After(next) {
			continue
		}
		kept = append(kept, r)
	}
	return kept, len(rows) - len(kept)
}

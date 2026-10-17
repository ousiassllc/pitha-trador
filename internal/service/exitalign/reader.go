package exitalign

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
)

// Comparer replays the labeled decisions of a window under the actual exit
// (*calibration.ExitReplayer implements it).
type Comparer interface {
	Compare(ctx context.Context, from, to time.Time, rule ExitConfig) (LabelExitComparison, error)
}

// ModelReporter returns the newest meta-labeling model's report, or ok=false
// when none was ever trained (*metalabel.Reporter implements it).
type ModelReporter interface {
	Report(ctx context.Context) (metamodel.Report, bool, error)
}

// Settings is the runtime_settings read access Reader needs.
type Settings interface {
	barriersettings.Store
	costsettings.Store
}

// Reader builds the Calibration screen's label-versus-exit view (issue #774).
type Reader struct {
	settings  Settings
	exit      ExitConfig
	comparer  Comparer
	reporter  ModelReporter
	positions ClosedReturnSource
	now       func() time.Time
}

// NewReader returns a Reader for the actual exit exit, reading the label and
// cost settings from settings on every call. comparer, reporter and positions
// may be nil: the table and warnings are still produced, without the replay
// comparison or the OOS comparison.
func NewReader(settings Settings, exit ExitConfig, comparer Comparer, reporter ModelReporter, positions ClosedReturnSource) *Reader {
	return &Reader{settings: settings, exit: exit, comparer: comparer, reporter: reporter, positions: positions, now: time.Now}
}

// WithClock overrides the clock (default time.Now), for tests.
func (r *Reader) WithClock(now func() time.Time) *Reader {
	r.now = now
	return r
}

// Alignment returns the table, the warnings, the comparison of the decisions of
// the last window and the OOS-versus-paper comparison.
func (r *Reader) Alignment(ctx context.Context, window time.Duration) (LabelExitAlignment, error) {
	barriers, err := barriersettings.Load(ctx, r.settings)
	if err != nil {
		return LabelExitAlignment{}, fmt.Errorf("exitalign: load triple-barrier settings: %w", err)
	}
	costs, err := costsettings.Load(ctx, r.settings)
	if err != nil {
		return LabelExitAlignment{}, fmt.Errorf("exitalign: load cost settings: %w", err)
	}
	out := LabelExitAlignment{ToleranceBps: ToleranceBps, ToleranceMinutes: ToleranceMinutes}
	out.Rows, out.Warnings = Correspondence(barriers, r.exit, costs)

	if r.comparer != nil {
		to := r.now()
		comparison, err := r.comparer.Compare(ctx, to.Add(-window), to, r.exit)
		if err != nil {
			return LabelExitAlignment{}, fmt.Errorf("exitalign: compare labels with the actual exit: %w", err)
		}
		out.Comparison = &comparison
	}
	if out.OOSPaper, err = r.oosPaper(ctx); err != nil {
		return LabelExitAlignment{}, err
	}
	return out, nil
}

// oosPaper sets the newest model's OOS EV beside the mean realized return of
// the paper positions closed in its OOS window. nil without a model.
func (r *Reader) oosPaper(ctx context.Context) (*OOSPaperComparison, error) {
	if r.reporter == nil {
		return nil, nil
	}
	report, ok, err := r.reporter.Report(ctx)
	if err != nil {
		return nil, fmt.Errorf("exitalign: load meta model report: %w", err)
	}
	if !ok {
		return nil, nil
	}
	cmp := &OOSPaperComparison{ModelVersion: report.Version, OOSStart: report.OOSStart, OOSEnd: report.OOSEnd, OOSEVBps: report.OOSEVBps}
	if r.positions == nil {
		return cmp, nil
	}
	closed, err := r.positions.ListClosedReturns(ctx, report.OOSStart, report.OOSEnd)
	if err != nil {
		return nil, fmt.Errorf("exitalign: read closed paper positions of the OOS window: %w", err)
	}
	cmp.PaperTrades = len(closed)
	if len(closed) == 0 {
		return cmp, nil
	}
	var sum float64
	for _, c := range closed {
		sum += c.ReturnBps
	}
	mean := sum / float64(len(closed))
	cmp.PaperMeanBps = &mean
	if report.OOSEVBps != nil {
		diff := mean - *report.OOSEVBps
		cmp.DiffBps = &diff
	}
	return cmp, nil
}

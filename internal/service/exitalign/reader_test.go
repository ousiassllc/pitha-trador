package exitalign_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

type settingsStore map[string]string

func (s settingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

type fakeComparer struct {
	from, to time.Time
	rule     exitalign.ExitConfig
	err      error
}

func (f *fakeComparer) Compare(_ context.Context, from, to time.Time, rule exitalign.ExitConfig) (exitalign.LabelExitComparison, error) {
	f.from, f.to, f.rule = from, to, rule
	return exitalign.LabelExitComparison{From: from, To: to, ReplayedCount: 7}, f.err
}

type fakeReporter struct {
	report domain.MetaModelReport
	ok     bool
}

func (f fakeReporter) Report(context.Context) (domain.MetaModelReport, bool, error) {
	return f.report, f.ok, nil
}

type fakePositions struct {
	returns  []trading.ClosedReturn
	from, to time.Time
}

func (f *fakePositions) ListClosedReturns(_ context.Context, from, to time.Time) ([]trading.ClosedReturn, error) {
	f.from, f.to = from, to
	return f.returns, nil
}

func ptr[T any](v T) *T { return &v }

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func TestReader_AlignmentWarnsAndCompares(t *testing.T) {
	comparer := &fakeComparer{}
	reader := exitalign.NewReader(settingsStore{}, shippedExit(), comparer, nil, nil).WithClock(func() time.Time { return now })
	got, err := reader.Alignment(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Alignment: %v", err)
	}
	if len(got.Rows) != 5 || len(got.Warnings) != 3 {
		t.Fatalf("rows/warnings = %d/%d, want 5/3 at the defaults", len(got.Rows), len(got.Warnings))
	}
	if got.ToleranceBps != exitalign.ToleranceBps || got.ToleranceMinutes != exitalign.ToleranceMinutes {
		t.Errorf("tolerances = %v/%v", got.ToleranceBps, got.ToleranceMinutes)
	}
	if !comparer.to.Equal(now) || !comparer.from.Equal(now.Add(-30*24*time.Hour)) {
		t.Errorf("compared window = [%v, %v), want the last 30 days", comparer.from, comparer.to)
	}
	if comparer.rule != shippedExit() {
		t.Errorf("replay rule = %+v, want the shipped exit", comparer.rule)
	}
	if got.Comparison == nil || got.Comparison.ReplayedCount != 7 || got.OOSPaper != nil {
		t.Errorf("comparison = %+v, oos = %+v; want the comparison and no OOS block without a model", got.Comparison, got.OOSPaper)
	}

	// Changing the barrier settings changes the warnings on the next read.
	store := settingsStore{barriersettings.KeyTakeProfitBps: "110", barriersettings.KeyStopLossBps: "70", barriersettings.KeyTimeoutMinutes: "20"}
	aligned, err := exitalign.NewReader(store, shippedExit(), nil, nil, nil).Alignment(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("Alignment: %v", err)
	}
	if len(aligned.Warnings) != 0 || aligned.Comparison != nil {
		t.Errorf("warnings = %v, comparison = %v; want none (label 110+10 = 120 bps, 70-10 = 60 bps, 20 min)", aligned.Warnings, aligned.Comparison)
	}
}

func TestReader_AlignmentPropagatesCompareError(t *testing.T) {
	boom := errors.New("boom")
	reader := exitalign.NewReader(settingsStore{}, shippedExit(), &fakeComparer{err: boom}, nil, nil)
	if _, err := reader.Alignment(context.Background(), time.Hour); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}

// The model's OOS EV is shown beside the mean realized paper return over the
// same window; without closed positions there is no mean ("データ無し").
func TestReader_OOSPaper(t *testing.T) {
	ctx := context.Background()
	report := domain.MetaModelReport{Version: "v1", OOSStart: now.Add(-48 * time.Hour), OOSEnd: now.Add(-24 * time.Hour), OOSEVBps: ptr(12.0)}

	none := &fakePositions{}
	got, err := exitalign.NewReader(settingsStore{}, shippedExit(), nil, fakeReporter{report, true}, none).Alignment(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Alignment: %v", err)
	}
	if got.OOSPaper == nil || got.OOSPaper.PaperMeanBps != nil || got.OOSPaper.PaperTrades != 0 || got.OOSPaper.DiffBps != nil {
		t.Fatalf("OOSPaper without positions = %+v, want no paper mean", got.OOSPaper)
	}
	if got.OOSPaper.OOSEVBps == nil || *got.OOSPaper.OOSEVBps != 12 || got.OOSPaper.ModelVersion != "v1" {
		t.Errorf("OOSPaper = %+v, want the model's EV 12 of v1", got.OOSPaper)
	}
	if !none.from.Equal(report.OOSStart) || !none.to.Equal(report.OOSEnd) {
		t.Errorf("positions read over [%v, %v), want the OOS window", none.from, none.to)
	}

	some := &fakePositions{returns: []trading.ClosedReturn{{ReturnBps: 30}, {ReturnBps: -50}, {ReturnBps: 10}}}
	got, err = exitalign.NewReader(settingsStore{}, shippedExit(), nil, fakeReporter{report, true}, some).Alignment(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Alignment: %v", err)
	}
	if o := got.OOSPaper; o.PaperTrades != 3 || o.PaperMeanBps == nil || *o.PaperMeanBps != -10.0/3 || o.DiffBps == nil || *o.DiffBps != -10.0/3-12 {
		t.Fatalf("OOSPaper = %+v, want mean -10/3 and diff mean-12 over 3 trades", got.OOSPaper)
	}

	undefined := report
	undefined.OOSEVBps = nil
	got, err = exitalign.NewReader(settingsStore{}, shippedExit(), nil, fakeReporter{undefined, true}, some).Alignment(ctx, time.Hour)
	if err != nil || got.OOSPaper.PaperMeanBps == nil || got.OOSPaper.DiffBps != nil {
		t.Fatalf("OOSPaper with an undefined EV = %+v, %v; want a mean and no diff", got.OOSPaper, err)
	}
}

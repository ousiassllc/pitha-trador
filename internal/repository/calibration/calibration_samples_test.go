package calibration_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
)

// seedHorizons labels one decision at the 5/10/15-minute horizons plus the
// legacy 20-minute one, so each horizon has its own distinguishable
// future_return (0.05/0.10/0.15/0.20).
func seedHorizons(t *testing.T, at time.Time) *calibration.CalibrationRepository {
	t.Helper()
	repo, decisions, instID := newCalibrationFixtures(t)
	d := insertTraderDecision(t, decisions, instID, at, domain.JevDirectionLong)
	for _, h := range []int{5, 10, 15, 20} {
		if _, err := repo.Insert(context.Background(), domain.CalibrationOutcome{
			JevDecisionID: d.ID, HorizonMinutes: h, FutureReturn: float64(h) / 100, WasDirectionCorrect: ptr(true),
		}); err != nil {
			t.Fatalf("seed %dm outcome: %v", h, err)
		}
	}
	return repo
}

func horizonsOf(samples []domain.LabeledSample) []int {
	var got []int
	for _, s := range samples {
		got = append(got, s.HorizonMinutes)
	}
	sort.Ints(got)
	return got
}

// Issue #719: the horizon filter keeps a per-horizon Calibration view from
// mixing 5/10/15-minute labels with the legacy 20-minute ones, and each
// sample reports its horizon.
func TestCalibrationRepository_ListLabeledSamples_FiltersByHorizon(t *testing.T) {
	repo := seedHorizons(t, time.Now().UTC())
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		horizons []int
		want     []int
	}{
		{"single horizon", []int{10}, []int{10}},
		{"current horizons exclude legacy 20m", []int{5, 10, 15}, []int{5, 10, 15}},
		{"no restriction returns every horizon", nil, []int{5, 10, 15, 20}},
		{"unlabeled horizon yields nothing", []int{30}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples, err := repo.ListLabeledSamples(ctx, tc.horizons)
			if err != nil {
				t.Fatalf("ListLabeledSamples: %v", err)
			}
			if got := horizonsOf(samples); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("horizons = %v, want %v", got, tc.want)
			}
			for _, s := range samples {
				if s.FutureReturn != float64(s.HorizonMinutes)/100 {
					t.Fatalf("sample %+v: FutureReturn does not belong to its HorizonMinutes", s)
				}
			}
		})
	}
}

func TestCalibrationRepository_ListLabeledSamplesSince_FiltersByHorizon(t *testing.T) {
	repo := seedHorizons(t, time.Now().UTC())
	ctx := context.Background()
	since := time.Now().UTC().Add(-time.Hour)

	samples, err := repo.ListLabeledSamplesSince(ctx, since, []int{5, 10, 15})
	if err != nil {
		t.Fatalf("ListLabeledSamplesSince: %v", err)
	}
	if got, want := horizonsOf(samples), []int{5, 10, 15}; !reflect.DeepEqual(got, want) {
		t.Fatalf("horizons = %v, want %v (legacy 20m must not mix in)", got, want)
	}

	future, err := repo.ListLabeledSamplesSince(ctx, time.Now().UTC().Add(time.Hour), []int{5})
	if err != nil {
		t.Fatalf("ListLabeledSamplesSince: %v", err)
	}
	if len(future) != 0 {
		t.Fatalf("ListLabeledSamplesSince(future) = %+v, want none (timestamp condition still applies)", future)
	}
}

// Issue #762: a triple-barrier label's horizon is the Settings timeout, so
// the "current labels" listings return every triple-barrier label whatever
// its horizon, while legacy close-only labels stay restricted to horizons.
func TestCalibrationRepository_ListCurrentLabeledSamples_IncludesTripleBarrierOfAnyHorizon(t *testing.T) {
	repo, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()
	d := insertTraderDecision(t, decisions, instID, now, domain.JevDirectionLong)
	for _, o := range []domain.CalibrationOutcome{
		{HorizonMinutes: 10, FutureReturn: 0.1, WasDirectionCorrect: ptr(true)},
		{HorizonMinutes: 20, FutureReturn: 0.2, WasDirectionCorrect: ptr(true)}, // legacy 20m: excluded
		{
			HorizonMinutes: 7, FutureReturn: 0.07, WasDirectionCorrect: ptr(true), // Settings timeout of 7 minutes
			BarrierHit: domain.BarrierHitTimeout, NetReturnBps: ptr(5.0), EntryRefPrice: ptr(1000.0),
			LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		},
	} {
		o.JevDecisionID = d.ID
		if _, err := repo.Insert(ctx, o); err != nil {
			t.Fatalf("seed %dm outcome: %v", o.HorizonMinutes, err)
		}
	}

	all, err := repo.ListCurrentLabeledSamples(ctx, []int{5, 10, 15})
	if err != nil {
		t.Fatalf("ListCurrentLabeledSamples: %v", err)
	}
	if got, want := horizonsOf(all), []int{7, 10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCurrentLabeledSamples horizons = %v, want %v", got, want)
	}
	since, err := repo.ListCurrentLabeledSamplesSince(ctx, now.Add(-time.Hour), []int{5, 10, 15})
	if err != nil {
		t.Fatalf("ListCurrentLabeledSamplesSince: %v", err)
	}
	if got, want := horizonsOf(since), []int{7, 10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCurrentLabeledSamplesSince horizons = %v, want %v", got, want)
	}
	// The plain listing is unchanged: only the requested horizons.
	plain, err := repo.ListLabeledSamples(ctx, []int{5, 10, 15})
	if err != nil {
		t.Fatalf("ListLabeledSamples: %v", err)
	}
	if got, want := horizonsOf(plain), []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListLabeledSamples horizons = %v, want %v", got, want)
	}
}

// Issue #881: with a scope, the samples of the foreign prompt versions are left
// out of ListLabeledSamples* and of the count Policy gates on (the all-versions
// count, AllVersions(), Paper's fallback, is the one read that keeps them), and a failed scope
// is an error rather than a mixed result.
func TestCalibrationRepository_ListLabeledSamples_LeavesOutForeignVersions(t *testing.T) {
	repo, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	at := time.Now().UTC()
	for i, version := range []string{"trader-v3", "trader-v4"} {
		confidence := 0.8
		d, err := decisions.Insert(ctx, domain.JevDecision{
			InstrumentID: instID, Symbol: "7203", Timestamp: at.Add(time.Duration(i) * time.Minute), DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "h" + version, StateJSON: "{}", QuestionVersion: version, ResponseJSON: "{}",
			Direction: ptr(domain.JevDirectionLong), Confidence: &confidence, ModelID: "m",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Insert(ctx, domain.CalibrationOutcome{JevDecisionID: d.ID, HorizonMinutes: 5, FutureReturn: 1 + float64(i), WasDirectionCorrect: ptr(true)}); err != nil {
			t.Fatal(err)
		}
	}
	since := at.Add(-time.Hour)
	reads := map[string]func() ([]domain.LabeledSample, error){
		"ListLabeledSamples":        func() ([]domain.LabeledSample, error) { return repo.ListLabeledSamples(ctx, []int{5}) },
		"ListCurrentLabeledSamples": func() ([]domain.LabeledSample, error) { return repo.ListCurrentLabeledSamples(ctx, []int{5}) },
		"ListLabeledSamplesSince":   func() ([]domain.LabeledSample, error) { return repo.ListLabeledSamplesSince(ctx, since, []int{5}) },
		"ListCurrentLabeledSamplesSince": func() ([]domain.LabeledSample, error) {
			return repo.ListCurrentLabeledSamplesSince(ctx, since, []int{5})
		},
	}
	futureReturns := func(samples []domain.LabeledSample) []float64 {
		var out []float64
		for _, s := range samples {
			out = append(out, s.FutureReturn)
		}
		sort.Float64s(out)
		return out
	}

	// No scope: both versions mix (the behaviour before the scope existed).
	for name, read := range reads {
		if got, err := read(); err != nil || !reflect.DeepEqual(futureReturns(got), []float64{1, 2}) {
			t.Fatalf("%s without a scope = %v, %v, want both versions", name, futureReturns(got), err)
		}
	}

	repo.SetForeignTraderVersions(func(context.Context) ([]string, error) { return []string{"trader-v4"}, nil })
	for name, read := range reads {
		if got, err := read(); err != nil || !reflect.DeepEqual(futureReturns(got), []float64{1}) {
			t.Fatalf("%s with trader-v4 foreign = %v, %v, want the trader-v3 sample only", name, futureReturns(got), err)
		}
	}

	repo.SetForeignTraderVersions(func(context.Context) ([]string, error) { return []string{"trader-v3"}, nil })
	if got, err := repo.ListLabeledSamples(ctx, []int{5}); err != nil || !reflect.DeepEqual(futureReturns(got), []float64{2}) {
		t.Fatalf("after a promotion = %v, %v, want the trader-v4 sample only", futureReturns(got), err)
	}
	// Policy's bucket-size gate counts the live version's cohort (trader-v4) only;
	// the all-versions count is Paper's fallback.
	if n, err := repo.CountLabeledSamplesInConfidenceRange(ctx, 0.5, 1, true, 10); err != nil || n != 1 {
		t.Fatalf("CountLabeledSamplesInConfidenceRange = %d, %v, want 1 (trader-v4 only)", n, err)
	}
	if n, err := repo.AllVersions().CountLabeledSamplesInConfidenceRange(ctx, 0.5, 1, true, 10); err != nil || n != 2 {
		t.Fatalf("AllVersions().CountLabeledSamplesInConfidenceRange = %d, %v, want 2", n, err)
	}

	repo.SetForeignTraderVersions(func(context.Context) ([]string, error) { return nil, errors.New("settings unreadable") })
	if n, err := repo.CountLabeledSamplesInConfidenceRange(ctx, 0.5, 1, true, 10); err == nil {
		t.Fatalf("CountLabeledSamplesInConfidenceRange = %d, nil with an unreadable scope, want an error", n)
	}
	if n, err := repo.AllVersions().CountLabeledSamplesInConfidenceRange(ctx, 0.5, 1, true, 10); err != nil || n != 2 {
		t.Fatalf("AllVersions().CountLabeledSamplesInConfidenceRange = %d, %v, want 2 (the scope is not read)", n, err)
	}
	if got, err := repo.ListLabeledSamples(ctx, []int{5}); err == nil {
		t.Fatalf("ListLabeledSamples = %v, nil with an unreadable scope, want an error", got)
	}
}

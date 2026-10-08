package calibration_test

import (
	"context"
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

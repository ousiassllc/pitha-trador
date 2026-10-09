package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

// Issue #719: Service.Metrics aggregates one horizon at a time; AllHorizons
// covers 5/10/15 together and never the legacy 20 minutes. Every horizon
// has a different outcome so a leaked horizon changes the counts.
func TestService_Metrics_AggregatesPerHorizon(t *testing.T) {
	f := newLabelerFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

	d := f.insertDecision(t, base, domain.JevDirectionLong, 0.85)
	for horizon, correct := range map[int]bool{5: true, 10: true, 15: false, 20: false} {
		if _, err := f.outcomes.Insert(ctx, domain.CalibrationOutcome{
			JevDecisionID: d.ID, HorizonMinutes: horizon, FutureReturn: 1, WasDirectionCorrect: ptr(correct),
		}); err != nil {
			t.Fatalf("insert %dm outcome: %v", horizon, err)
		}
	}
	svc := calibration.NewService(f.outcomes, noTrades{})

	for _, tc := range []struct {
		name         string
		horizon      int
		wantSamples  int
		wantAccuracy float64
	}{
		{"5m", 5, 1, 1},
		{"15m", 15, 1, 0},
		{"all excludes the legacy 20m", calibration.AllHorizons, 3, 2.0 / 3},
		{"legacy 20m alone is still selectable", 20, 1, 0},
		{"unlabeled horizon is empty", 30, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics, err := svc.Metrics(ctx, tc.horizon)
			if err != nil {
				t.Fatalf("Metrics(%d): %v", tc.horizon, err)
			}
			if metrics.SampleCount != tc.wantSamples {
				t.Fatalf("SampleCount = %d, want %d", metrics.SampleCount, tc.wantSamples)
			}
			if tc.wantSamples == 0 {
				return
			}
			var got domain.ConfidenceBucket
			for _, b := range metrics.Buckets {
				if b.SampleCount > 0 {
					got = b
				}
			}
			if got.SampleCount != tc.wantSamples || got.DirectionAccuracy < tc.wantAccuracy-1e-9 || got.DirectionAccuracy > tc.wantAccuracy+1e-9 {
				t.Fatalf("populated bucket = %+v, want %d samples at accuracy %v", got, tc.wantSamples, tc.wantAccuracy)
			}
		})
	}
}

// The daily self-improvement analysis (DirectionMetricsSince) uses only the
// current horizons too: a legacy 20-minute label must not leak into it.
func TestService_DirectionMetricsSince_ExcludesLegacyHorizon(t *testing.T) {
	f := newLabelerFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

	d := f.insertDecision(t, base, domain.JevDirectionLong, 0.85)
	for _, horizon := range []int{5, 20} {
		if _, err := f.outcomes.Insert(ctx, domain.CalibrationOutcome{
			JevDecisionID: d.ID, HorizonMinutes: horizon, FutureReturn: 1, WasDirectionCorrect: ptr(true),
		}); err != nil {
			t.Fatalf("insert %dm outcome: %v", horizon, err)
		}
	}
	svc := calibration.NewService(f.outcomes, noTrades{})

	long, short, err := svc.DirectionMetricsSince(ctx, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("DirectionMetricsSince: %v", err)
	}
	if long.SampleCount != 1 || short.SampleCount != 0 {
		t.Fatalf("SampleCount long/short = %d/%d, want 1/0 (legacy 20m excluded)", long.SampleCount, short.SampleCount)
	}
}

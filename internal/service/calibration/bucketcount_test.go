package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

type noTrades struct{}

func (noTrades) List(context.Context) ([]domain.DecisionTrade, error) { return nil, nil }

// BucketHasSamples must classify every confidence into the same bucket
// that Metrics counts it into, including the bucket edges and the
// closed-high last bucket, and flip exactly at the threshold, so the
// policy's calibrated() verdict is unchanged by the capped database
// COUNT (count == min-1 / min / min+1).
func TestService_BucketHasSamples_MatchesMetricsBuckets(t *testing.T) {
	f := newLabelerFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	confidences := []float64{0.45, 0.50, 0.55, 0.60, 0.65, 0.70, 0.79, 0.80, 0.90, 0.95, 1.00}
	for i, c := range confidences {
		d := f.insertDecision(t, base.Add(time.Duration(i)*time.Minute), domain.JevDirectionLong, c)
		for _, horizon := range []int{5, 15} {
			if _, err := f.outcomes.Insert(ctx, domain.CalibrationOutcome{
				JevDecisionID: d.ID, HorizonMinutes: horizon, WasDirectionCorrect: ptr(true),
			}); err != nil {
				t.Fatalf("insert outcome: %v", err)
			}
		}
	}

	svc := calibration.NewService(f.outcomes, noTrades{})
	metrics, err := svc.Metrics(ctx, calibration.AllHorizons)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	for _, c := range confidences {
		want := 0
		for i, r := range domain.DefaultConfidenceBucketRanges {
			last := i == len(domain.DefaultConfidenceBucketRanges)-1
			if c >= r.Low && (c < r.High || (last && c == r.High)) {
				want = metrics.Buckets[i].SampleCount
			}
		}
		if want == 0 {
			if got, err := svc.BucketHasSamples(ctx, c, 1); err != nil || got {
				t.Errorf("BucketHasSamples(%v, 1) = %v, %v; want false (no samples)", c, got, err)
			}
			continue
		}
		for _, tc := range []struct {
			min  int
			want bool
		}{{want - 1, true}, {want, true}, {want + 1, false}} {
			if tc.min <= 0 {
				continue
			}
			got, err := svc.BucketHasSamples(ctx, c, tc.min)
			if err != nil {
				t.Fatalf("BucketHasSamples(%v, %d): %v", c, tc.min, err)
			}
			if got != tc.want {
				t.Errorf("BucketHasSamples(%v, %d) = %v, want %v (Metrics bucket count %d)", c, tc.min, got, tc.want, want)
			}
		}
	}
	if got, _ := svc.BucketHasSamples(ctx, 0.45, 1); got {
		t.Error("BucketHasSamples(0.45, 1) = true, want false (below every bucket)")
	}
	// 0.90, 0.95 and 1.00, each with 2 horizons: the last bucket includes its upper edge.
	if got, _ := svc.BucketHasSamples(ctx, 1.0, 6); !got {
		t.Error("BucketHasSamples(1.0, 6) = false, want true")
	}
	if got, _ := svc.BucketHasSamples(ctx, 1.0, 7); got {
		t.Error("BucketHasSamples(1.0, 7) = true, want false")
	}
}

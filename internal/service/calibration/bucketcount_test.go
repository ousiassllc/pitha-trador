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

// BucketSampleCount must classify every confidence into the same bucket
// that Metrics counts it into, including the bucket edges and the
// closed-high last bucket, so the policy's calibrated() verdict is
// unchanged by reading the count straight from the database.
func TestService_BucketSampleCount_MatchesMetricsBuckets(t *testing.T) {
	f := newLabelerFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	confidences := []float64{0.45, 0.50, 0.55, 0.60, 0.65, 0.70, 0.79, 0.80, 0.90, 0.95, 1.00}
	for i, c := range confidences {
		d := f.insertDecision(t, base.Add(time.Duration(i)*time.Minute), domain.JevDirectionLong, c)
		for _, horizon := range []int{5, 20} {
			if _, err := f.outcomes.Insert(ctx, domain.CalibrationOutcome{
				JevDecisionID: d.ID, HorizonMinutes: horizon, WasDirectionCorrect: ptr(true),
			}); err != nil {
				t.Fatalf("insert outcome: %v", err)
			}
		}
	}

	svc := calibration.NewService(f.outcomes, noTrades{})
	metrics, err := svc.Metrics(ctx)
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
		got, err := svc.BucketSampleCount(ctx, c)
		if err != nil {
			t.Fatalf("BucketSampleCount(%v): %v", c, err)
		}
		if got != want {
			t.Errorf("BucketSampleCount(%v) = %d, want %d (Metrics bucket count)", c, got, want)
		}
	}
	if got, _ := svc.BucketSampleCount(ctx, 0.45); got != 0 {
		t.Errorf("BucketSampleCount(0.45) = %d, want 0 (below every bucket)", got)
	}
	if got, _ := svc.BucketSampleCount(ctx, 1.0); got != 6 {
		t.Errorf("BucketSampleCount(1.0) = %d, want 6 (0.90, 0.95 and 1.00 x2 horizons: last bucket includes its upper edge)", got)
	}
}

package calibration

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// BucketHasSamples reports whether the domain.DefaultConfidenceBucketRanges
// bucket that contains confidence (the same bucket Metrics counts samples
// into) holds at least min labeled samples; false when confidence lies in
// no bucket. Unlike Metrics it runs one capped COUNT in the database: no
// sample list, no closed-position PnL join, and the scan stops at min
// matches, so its cost does not grow with the labeling history.
func (s *Service) BucketHasSamples(ctx context.Context, confidence float64, min int) (bool, error) {
	ranges := domain.DefaultConfidenceBucketRanges
	i, ok := bucketIndex(ranges, confidence)
	if !ok {
		return false, nil
	}
	n, err := s.outcomes.CountLabeledSamplesInConfidenceRange(ctx, ranges[i].Low, ranges[i].High, i == len(ranges)-1, min)
	if err != nil {
		return false, fmt.Errorf("calibration: count samples in bucket %s: %w", ranges[i].Range, err)
	}
	return n >= min, nil
}

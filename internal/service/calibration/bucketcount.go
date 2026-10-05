package calibration

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// BucketSampleCount returns how many labeled samples fall in the
// domain.DefaultConfidenceBucketRanges bucket that contains confidence
// (the same bucket Metrics counts them into), or 0 when confidence lies in
// no bucket. Unlike Metrics it runs one COUNT in the database: no sample
// list, no closed-position PnL join.
func (s *Service) BucketSampleCount(ctx context.Context, confidence float64) (int, error) {
	ranges := domain.DefaultConfidenceBucketRanges
	i, ok := bucketIndex(ranges, confidence)
	if !ok {
		return 0, nil
	}
	n, err := s.outcomes.CountLabeledSamplesInConfidenceRange(ctx, ranges[i].Low, ranges[i].High, i == len(ranges)-1)
	if err != nil {
		return 0, fmt.Errorf("calibration: count samples in bucket %s: %w", ranges[i].Range, err)
	}
	return n, nil
}

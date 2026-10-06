package jevflow_test

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func solInput() assist.SolAnalysisInput {
	return assist.SolAnalysisInput{
		Long: assist.DirectionCalibration{
			Thresholds: config.PolicyDirectionThresholds{MinProbability: 0.60, MinEntryQuality: domain.JevEntryQualityGood},
			Calibration: domain.CalibrationMetrics{
				Buckets:     []domain.ConfidenceBucket{{Range: "0.60-0.70", DirectionAccuracy: 0.4, AvgFutureReturnPct: -0.5, SampleCount: 40}},
				SampleCount: 40,
			},
		},
	}
}

func reviewInput(baseExp, candExp, baseDD, candDD float64) assist.OpusReviewInput {
	return assist.OpusReviewInput{
		RationaleJSON: `{"why":"x"}`,
		Changes:       []domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.65"}},
		Comparison: assist.BacktestComparison{
			BaselineExpectancy: baseExp, CandidateExpectancy: candExp,
			BaselineMaxDrawdownPct: baseDD, CandidateMaxDrawdownPct: candDD,
			BaselineTradeCount: 30, CandidateTradeCount: 30,
		},
	}
}

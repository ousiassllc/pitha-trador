// Package rankingmeasure is the opt-in measurement loop of issue #652
// (#651 段階0): it periodically calls kabuステーション GET /ranking and logs
// only measurement values - 件数・duration_ms・CurrentPriceTime・HTTP/kabuコード.
// The ranking's prices and symbols are never stored or logged (kabu利用規約,
// kabusapi#1343): marketdata.Client.MeasureRanking already reduces each
// response to counts and the 時刻 before it reaches this package.
package rankingmeasure

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// Ranker fetches one ranking measurement (marketdata.Client).
type Ranker interface {
	MeasureRanking(ctx context.Context, rankType int, exchange string) (marketdata.RankingMeasurement, error)
}

// Measurer measures every Types x Exchanges ranking once per Cycle.
type Measurer struct {
	Ranker    Ranker
	Types     []int
	Exchanges []string
	// Open reports whether t is inside a trading session. nil measures at any
	// time (scan.ranking_measure.include_outside_session).
	Open func(t time.Time) bool
	// Now defaults to time.Now.
	Now func() time.Time
}

func (m Measurer) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Cycle measures every type x exchange pair once and returns how many
// requests it made (0 outside the session). A failure or panic on one pair
// is logged as a measurement and does not stop the others.
func (m Measurer) Cycle(ctx context.Context) int {
	if m.Open != nil && !m.Open(m.now()) {
		return 0
	}
	requests := 0
	for _, rankType := range m.Types {
		for _, exchange := range m.Exchanges {
			if ctx.Err() != nil {
				return requests
			}
			safego.Run("ranking measure", func() { m.measure(ctx, rankType, exchange) })
			requests++
		}
	}
	return requests
}

// measure logs one measurement line. Only counts, durations, the HH:mm
// 時刻 and the error codes are logged - never a price or symbol.
func (m Measurer) measure(ctx context.Context, rankType int, exchange string) {
	start := time.Now()
	res, err := m.Ranker.MeasureRanking(ctx, rankType, exchange)
	attrs := []any{
		"type", rankType, "exchange", exchange,
		"duration_ms", time.Since(start).Milliseconds(),
	}
	if err != nil {
		attrs = append(attrs, "ok", false)
		var api *marketdata.APIError
		if errors.As(err, &api) {
			attrs = append(attrs, "http_status", api.StatusCode, "kabu_code", api.Code)
		}
		attrs = append(attrs, "rate_limited", broker.IsRateLimited(err), "error", err)
		slog.Warn("rankingmeasure: ranking measured", attrs...)
		return
	}
	attrs = append(attrs, "ok", true, "result_count", res.Count,
		"duplicate_ranks", res.DuplicateRanks, "current_price_time", res.LatestPriceTime)
	slog.Info("rankingmeasure: ranking measured", attrs...)
}

// Run calls Cycle every interval until ctx is done.
func (m Measurer) Run(ctx context.Context, interval time.Duration) {
	safego.Loop(ctx, "ranking measure", func() time.Duration { return interval }, func(ctx context.Context) error {
		m.Cycle(ctx)
		return nil
	})
}

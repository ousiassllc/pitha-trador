package loader

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// Market is the replay layer's market data and the periods it is missing.
type Market struct {
	Instruments []replaylayer.Instrument
	// Unavailable are the JST days with independent decisions but no
	// market_snapshots bar of any replayed instrument, with the reason.
	Unavailable []replaylayer.Unavailable
	// SkippedInstruments counts instruments of the decisions that are not in
	// the copy or have no bar in the window (nothing of them is replayed).
	SkippedInstruments int
}

// LoadMarket reads, for every instrument that has one of decisions, its
// market_snapshots bars in [from, to) preceded by the live history lookback
// (featureengine.HistoryLookbackBars bars before from) as the look-ahead-check
// warmup, exactly as the backtest's source does (without raw_data_json). The
// replayed universe is the instruments of the independent decisions: an
// instrument without a decision cannot trade. Days that hold an independent
// decision but no bar of any replayed instrument are reported as unavailable;
// the label layer is not affected by them.
func (s *Source) LoadMarket(ctx context.Context, decisions []domain.JevDecision, from, to time.Time) (Market, error) {
	ids := map[int64]struct{}{}
	for _, d := range decisions {
		ids[d.InstrumentID] = struct{}{}
	}
	order := make([]int64, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	slices.Sort(order)
	instruments, snapshots := market.NewInstrumentRepository(s.db), market.NewSnapshotRepository(s.db)
	var m Market
	barDays := map[string]bool{}
	for _, id := range order {
		inst, err := instruments.Get(ctx, id)
		if errors.Is(err, market.ErrInstrumentNotFound) {
			m.SkippedInstruments++
			continue
		}
		if err != nil {
			return Market{}, fmt.Errorf("loader: read instrument %d: %w", id, err)
		}
		bars, err := snapshots.ListHistoryByInstrumentRange(ctx, id, from, to)
		if err != nil {
			return Market{}, fmt.Errorf("loader: read market_snapshots of %s: %w", inst.Symbol, err)
		}
		if len(bars) == 0 {
			m.SkippedInstruments++
			continue
		}
		warmup, err := snapshots.ListHistoryByInstrumentBefore(ctx, id, from, featureengine.HistoryLookbackBars)
		if err != nil {
			return Market{}, fmt.Errorf("loader: read market_snapshots history of %s: %w", inst.Symbol, err)
		}
		slices.Reverse(warmup)
		for _, b := range bars {
			barDays[dayKey(b.Timestamp)] = true
		}
		m.Instruments = append(m.Instruments, replaylayer.Instrument{
			InstrumentID: id, Symbol: inst.Symbol, AssetClass: inst.AssetClass,
			Snapshots: append(warmup, bars...), WarmupBars: len(warmup),
		})
	}
	m.Unavailable = missingDays(decisions, barDays)
	return m, nil
}

func dayKey(t time.Time) string { return t.In(marketcalendar.JST).Format("2006-01-02") }

// missingDays lists the days with decisions and no bar, oldest first.
func missingDays(decisions []domain.JevDecision, barDays map[string]bool) []replaylayer.Unavailable {
	count := map[string]int{}
	for _, d := range decisions {
		if k := dayKey(d.Timestamp); !barDays[k] {
			count[k]++
		}
	}
	days := make([]string, 0, len(count))
	for k := range count {
		days = append(days, k)
	}
	sort.Strings(days)
	out := make([]replaylayer.Unavailable, 0, len(days))
	for _, k := range days {
		start, err := time.ParseInLocation("2006-01-02", k, marketcalendar.JST)
		if err != nil {
			continue
		}
		out = append(out, replaylayer.Unavailable{
			From: start, To: start.AddDate(0, 0, 1),
			Reason: fmt.Sprintf("market_snapshots missing: %d independent decision(s) of the day have no bar", count[k]),
		})
	}
	return out
}

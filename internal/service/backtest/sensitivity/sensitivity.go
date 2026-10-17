// Package sensitivity answers "how much does the result depend on the cost
// assumption" (issue #779): it re-prices a backtest's trades with extra
// round-trip cost and reports the Expectancy and the share of trades that still
// make money. The extra cost is subtracted from each trade's NetReturnPct, so
// the 0 bps row is the backtest's own result under the Settings cost settings.
package sensitivity

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// DefaultExtraBps are the extra round-trip costs (bps) of the default table.
var DefaultExtraBps = []float64{0, 2, 5, 10}

// Row is one extra-cost scenario.
type Row struct {
	// ExtraCostBps is the round-trip cost added to every trade.
	ExtraCostBps float64
	// TradeCount is the number of trades (the same in every row).
	TradeCount int
	// ExpectancyPct is the mean net return per trade (%), after the extra cost;
	// ExpectancyBps is the same in bps.
	ExpectancyPct float64
	ExpectancyBps float64
	// PositiveShare is the share (0..1) of trades whose net return stays above
	// zero after the extra cost: the trades that are still worth taking. 0 when
	// there are no trades.
	PositiveShare float64
}

// Table returns one Row per element of extraBps (in the given order) over
// trades. With no trades every row has zero statistics.
func Table(trades []backtest.Trade, extraBps []float64) []Row {
	rows := make([]Row, len(extraBps))
	for i, extra := range extraBps {
		row := Row{ExtraCostBps: extra, TradeCount: len(trades)}
		extraPct := extra / 100
		var sum float64
		positive := 0
		for _, t := range trades {
			net := t.NetReturnPct - extraPct
			sum += net
			if net > 0 {
				positive++
			}
		}
		if len(trades) > 0 {
			row.ExpectancyPct = sum / float64(len(trades))
			row.ExpectancyBps = row.ExpectancyPct * 100
			row.PositiveShare = float64(positive) / float64(len(trades))
		}
		rows[i] = row
	}
	return rows
}

// WriteCSV writes rows as CSV with a header line, for export or printing.
func WriteCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"extra_cost_bps", "trade_count", "expectancy_pct", "expectancy_bps", "positive_share"}); err != nil {
		return err
	}
	for _, r := range rows {
		record := []string{
			strconv.FormatFloat(r.ExtraCostBps, 'f', -1, 64),
			strconv.Itoa(r.TradeCount),
			strconv.FormatFloat(r.ExpectancyPct, 'f', 4, 64),
			strconv.FormatFloat(r.ExpectancyBps, 'f', 2, 64),
			strconv.FormatFloat(r.PositiveShare, 'f', 4, 64),
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

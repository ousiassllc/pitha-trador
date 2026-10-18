// Package sensitivity answers "how much does the result depend on the cost
// assumption" (issue #779): it re-prices a backtest's trades with extra
// round-trip cost and reports the Expectancy and the share of trades that still
// make money. The extra cost is subtracted from each trade's NetReturnPct, so
// the 0 bps row is the backtest's own result under the Settings cost settings.
// It also reports the share of trades the meta-labeling model still predicted
// to be worth taking (EV above the extra cost), which is a different question
// from the realized one.
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
	// PositiveShare is the share (0..1) of trades whose realized replay net
	// return stays above zero after the extra cost. It says nothing about the
	// model's predicted EV (see PredictedEVPositiveShare). 0 when there are no
	// trades.
	PositiveShare float64
	// PredictedEVTradeCount is the number of trades that carry the model's
	// predicted EV at the entry decision (Trade.PredictedEVBps), the
	// denominator of PredictedEVPositiveShare (the same in every row). Trades
	// without one (the legacy confidence entry rule) are excluded.
	PredictedEVTradeCount int
	// PredictedEVPositiveShare is the share (0..1) of those trades whose
	// predicted EV (bps) minus the extra cost is above zero. 0 when
	// PredictedEVTradeCount is 0.
	PredictedEVPositiveShare float64
}

// Table returns one Row per element of extraBps (in the given order) over
// trades. With no trades every row has zero statistics.
func Table(trades []backtest.Trade, extraBps []float64) []Row {
	rows := make([]Row, len(extraBps))
	for i, extra := range extraBps {
		row := Row{ExtraCostBps: extra, TradeCount: len(trades)}
		extraPct := extra / 100
		var sum float64
		positive, predicted, predictedPositive := 0, 0, 0
		for _, t := range trades {
			net := t.NetReturnPct - extraPct
			sum += net
			if net > 0 {
				positive++
			}
			if t.PredictedEVBps != nil {
				predicted++
				if *t.PredictedEVBps-extra > 0 {
					predictedPositive++
				}
			}
		}
		if len(trades) > 0 {
			row.ExpectancyPct = sum / float64(len(trades))
			row.ExpectancyBps = row.ExpectancyPct * 100
			row.PositiveShare = float64(positive) / float64(len(trades))
		}
		row.PredictedEVTradeCount = predicted
		if predicted > 0 {
			row.PredictedEVPositiveShare = float64(predictedPositive) / float64(predicted)
		}
		rows[i] = row
	}
	return rows
}

// WriteCSV writes rows as CSV with a header line, for export or printing.
func WriteCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"extra_cost_bps", "trade_count", "expectancy_pct", "expectancy_bps", "positive_share", "predicted_ev_trade_count", "predicted_ev_positive_share"}); err != nil {
		return err
	}
	for _, r := range rows {
		record := []string{
			strconv.FormatFloat(r.ExtraCostBps, 'f', -1, 64),
			strconv.Itoa(r.TradeCount),
			strconv.FormatFloat(r.ExpectancyPct, 'f', 4, 64),
			strconv.FormatFloat(r.ExpectancyBps, 'f', 2, 64),
			strconv.FormatFloat(r.PositiveShare, 'f', 4, 64),
			strconv.Itoa(r.PredictedEVTradeCount),
			strconv.FormatFloat(r.PredictedEVPositiveShare, 'f', 4, 64),
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

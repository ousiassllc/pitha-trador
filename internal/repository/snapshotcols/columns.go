// Package snapshotcols maps the market_snapshots Feature columns to
// domain.Feature fields for SnapshotRepository's INSERT and SELECT. It is a
// separate package so the one-line-per-column table does not grow
// internal/repository past its directory size limit.
package snapshotcols

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Column ties one market_snapshots column to its Snapshot field:
// Arg yields the INSERT argument and Dest the Scan destination. Keeping
// both directions in one table keeps the INSERT column list, its
// placeholders and the SELECT/Scan order from drifting apart.
type Column struct {
	Name string
	Arg  func(*domain.Snapshot) any
	Dest func(*domain.Snapshot) any
}

func f64Col(name string, field func(*domain.Snapshot) **float64) Column {
	return Column{
		Name: name,
		Arg:  func(s *domain.Snapshot) any { return nullable(*field(s)) },
		Dest: func(s *domain.Snapshot) any { return nullFloat(field(s)) },
	}
}

func i64Col(name string, field func(*domain.Snapshot) **int64) Column {
	return Column{
		Name: name,
		Arg:  func(s *domain.Snapshot) any { return nullable(*field(s)) },
		Dest: func(s *domain.Snapshot) any { return nullInt64(field(s)) },
	}
}

func reqCol[T any](name string, field func(*domain.Snapshot) *T) Column {
	return Column{
		Name: name,
		Arg:  func(s *domain.Snapshot) any { return *field(s) },
		Dest: func(s *domain.Snapshot) any { return field(s) },
	}
}

// Columns is every market_snapshots column between
// turnover and raw_data_json, in table order for the original columns
// followed by those added in migration 000016.
var Columns = []Column{
	f64Col("return_1m", func(s *domain.Snapshot) **float64 { return &s.Feature.Return1m }),
	f64Col("return_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.Return5m }),
	f64Col("return_15m", func(s *domain.Snapshot) **float64 { return &s.Feature.Return15m }),
	reqCol("vwap", func(s *domain.Snapshot) *float64 { return &s.Feature.VWAP }),
	reqCol("price_vs_vwap_bps", func(s *domain.Snapshot) *float64 { return &s.Feature.PriceVsVWAPBps }),
	f64Col("volume_ratio_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.VolumeRatio5m }),
	f64Col("orderbook_imbalance", func(s *domain.Snapshot) **float64 { return &s.Feature.OrderbookImbalance }),
	f64Col("realized_vol_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.RealizedVol5m }),
	f64Col("market_return_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.MarketReturn5m }),
	f64Col("sector_return_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.SectorReturn5m }),

	f64Col("return_3m", func(s *domain.Snapshot) **float64 { return &s.Feature.Return3m }),
	f64Col("return_30m", func(s *domain.Snapshot) **float64 { return &s.Feature.Return30m }),
	f64Col("high_distance_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.HighDistance5m }),
	f64Col("low_distance_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.LowDistance5m }),
	f64Col("session_high_distance", func(s *domain.Snapshot) **float64 { return &s.Feature.SessionHighDistance }),
	f64Col("session_low_distance", func(s *domain.Snapshot) **float64 { return &s.Feature.SessionLowDistance }),
	f64Col("vwap_slope", func(s *domain.Snapshot) **float64 { return &s.Feature.VWAPSlope }),
	i64Col("vwap_cross_direction", func(s *domain.Snapshot) **int64 { return &s.Feature.VWAPCrossDirection }),
	i64Col("volume_1m", func(s *domain.Snapshot) **int64 { return &s.Feature.Volume1m }),
	i64Col("volume_5m", func(s *domain.Snapshot) **int64 { return &s.Feature.Volume5m }),
	f64Col("volume_ratio_1m", func(s *domain.Snapshot) **float64 { return &s.Feature.VolumeRatio1m }),
	f64Col("turnover_1m", func(s *domain.Snapshot) **float64 { return &s.Feature.Turnover1m }),
	f64Col("turnover_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.Turnover5m }),
	f64Col("atr_1m", func(s *domain.Snapshot) **float64 { return &s.Feature.ATR1m }),
	f64Col("atr_5m", func(s *domain.Snapshot) **float64 { return &s.Feature.ATR5m }),
	f64Col("realized_vol_15m", func(s *domain.Snapshot) **float64 { return &s.Feature.RealizedVol15m }),
	f64Col("volatility_expansion_ratio", func(s *domain.Snapshot) **float64 { return &s.Feature.VolatilityExpansionRatio }),
	f64Col("bid_depth", func(s *domain.Snapshot) **float64 { return &s.Feature.BidDepth }),
	f64Col("ask_depth", func(s *domain.Snapshot) **float64 { return &s.Feature.AskDepth }),
	f64Col("buy_trade_ratio", func(s *domain.Snapshot) **float64 { return &s.Feature.BuyTradeRatio }),
	f64Col("sell_trade_ratio", func(s *domain.Snapshot) **float64 { return &s.Feature.SellTradeRatio }),
	f64Col("trade_flow_imbalance", func(s *domain.Snapshot) **float64 { return &s.Feature.TradeFlowImbalance }),
	f64Col("microprice", func(s *domain.Snapshot) **float64 { return &s.Feature.Microprice }),
	f64Col("market_return_1m", func(s *domain.Snapshot) **float64 { return &s.Feature.MarketReturn1m }),
	f64Col("stock_vs_sector_relative_strength", func(s *domain.Snapshot) **float64 { return &s.Feature.StockVsSectorRelativeStrength }),
	f64Col("market_breadth", func(s *domain.Snapshot) **float64 { return &s.Feature.MarketBreadth }),
}

// Names is the comma-joined column list of Columns.
func Names() string {
	names := make([]string, len(Columns))
	for i, c := range Columns {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// Args returns Columns' INSERT arguments for s, in column order.
func Args(s *domain.Snapshot) []any {
	args := make([]any, len(Columns))
	for i, c := range Columns {
		args[i] = c.Arg(s)
	}
	return args
}

// Dests returns Columns' Scan destinations for s, in column order.
func Dests(s *domain.Snapshot) []any {
	dests := make([]any, len(Columns))
	for i, c := range Columns {
		dests[i] = c.Dest(s)
	}
	return dests
}

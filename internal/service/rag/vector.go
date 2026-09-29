package rag

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Dimensions is the fixed length of every standardized feature vector
// this package builds, matching market_snapshot_vectors/
// jev_decision_vectors' FLOAT[14] embedding column
// (docs/architecture/er.md §ベクトルインデックス（sqlite-vec）).
const Dimensions = 14

// Vector is one standardized 14-dimension feature embedding, in the
// fixed field order documented in er.md: return_1m, return_5m,
// return_15m, price_vs_vwap_bps, volume_ratio_1m, volume_ratio_5m,
// spread_bps, orderbook_imbalance, realized_vol_5m, realized_vol_15m,
// volatility_expansion_ratio, market_return_5m, sector_return_5m,
// stock_vs_sector_relative_strength.
type Vector [Dimensions]float64

// FeatureInput is the named source values Build standardizes into a
// Vector. It mirrors the field names in er.md's ベクトルインデックス
// section rather than any one persisted struct (domain.Feature,
// jev.ScoutState, ...), so callers building a vector for a
// market_snapshots row (FeatureInputFromFeature) or a jev_decisions row
// (jev package, from its own ScoutState) populate the same shape.
//
// Every field is a pointer: nil means "not computable this cycle"
// (functional.md FR-FE-2) and standardizes to 0 - the center of a
// standardized feature - rather than skewing the distance calculation
// with a placeholder magnitude.
type FeatureInput struct {
	Return1m                      *float64
	Return5m                      *float64
	Return15m                     *float64
	PriceVsVWAPBps                *float64
	VolumeRatio1m                 *float64
	VolumeRatio5m                 *float64
	SpreadBps                     *float64
	OrderbookImbalance            *float64
	RealizedVol5m                 *float64
	RealizedVol15m                *float64
	VolatilityExpansionRatio      *float64
	MarketReturn5m                *float64
	SectorReturn5m                *float64
	StockVsSectorRelativeStrength *float64
}

// FeatureInputFromFeature builds a FeatureInput from a Feature Engine
// result (internal/domain.Feature) plus its snapshot's spread_bps
// (domain.Snapshot.SpreadBps is a sibling field of Feature, not part of
// Feature itself - docs/architecture/er.md §market_snapshots).
func FeatureInputFromFeature(f domain.Feature, spreadBps *float64) FeatureInput {
	priceVsVWAPBps := f.PriceVsVWAPBps
	return FeatureInput{
		Return1m:           f.Return1m,
		Return5m:           f.Return5m,
		Return15m:          f.Return15m,
		PriceVsVWAPBps:     &priceVsVWAPBps,
		VolumeRatio1m:      f.VolumeRatio1m,
		VolumeRatio5m:      f.VolumeRatio5m,
		SpreadBps:          spreadBps,
		OrderbookImbalance: f.OrderbookImbalance,
		RealizedVol5m:      f.RealizedVol5m,
		RealizedVol15m:     f.RealizedVol15m,

		VolatilityExpansionRatio:      f.VolatilityExpansionRatio,
		MarketReturn5m:                f.MarketReturn5m,
		SectorReturn5m:                f.SectorReturn5m,
		StockVsSectorRelativeStrength: f.StockVsSectorRelativeStrength,
	}
}

// scales holds one fixed divisor per Vector dimension, chosen from each
// feature's typical magnitude (functional.md §4.1) so every dimension is
// roughly O(1) after division: a 1% return and a 50bps price_vs_vwap_bps
// reading should contribute comparable weight to sqlite-vec's L2
// distance, rather than the basis-point-scaled features dominating the
// raw-fraction ones. This is a fixed, deterministic standardization (no
// running mean/stdev store to keep up to date, no LLM call) appropriate
// for a same-process, zero added-latency embedding (FR-RAG-3).
var scales = Vector{
	0.01,  // return_1m: typical 1-minute move ~1%
	0.02,  // return_5m
	0.03,  // return_15m
	50,    // price_vs_vwap_bps
	1,     // volume_ratio_1m: ratio centered on 1.0
	1,     // volume_ratio_5m
	20,    // spread_bps
	0.3,   // orderbook_imbalance: already bounded to [-1, 1]
	0.01,  // realized_vol_5m
	0.015, // realized_vol_15m
	1,     // volatility_expansion_ratio: ratio centered on 1.0
	0.01,  // market_return_5m
	0.01,  // sector_return_5m
	0.5,   // stock_vs_sector_relative_strength
}

// Build standardizes in into a Vector (FR-RAG-1): each populated field is
// divided by its fixed scale (see scales); nil fields become 0.
func Build(in FeatureInput) Vector {
	raw := [Dimensions]*float64{
		in.Return1m, in.Return5m, in.Return15m, in.PriceVsVWAPBps,
		in.VolumeRatio1m, in.VolumeRatio5m, in.SpreadBps, in.OrderbookImbalance,
		in.RealizedVol5m, in.RealizedVol15m, in.VolatilityExpansionRatio,
		in.MarketReturn5m, in.SectorReturn5m, in.StockVsSectorRelativeStrength,
	}

	var v Vector
	for i, p := range raw {
		if p != nil {
			v[i] = *p / scales[i]
		}
	}
	return v
}

// json encodes v as the JSON-array text sqlite-vec accepts for both
// vec0 INSERTs and MATCH queries (modernc.org/sqlite@v1.59.0's
// vec_test.go binds a `[...]` string literal directly), so callers never
// need to hand-roll the extension's little-endian float32 blob format.
func (v Vector) json() (string, error) {
	b, err := json.Marshal(v[:])
	if err != nil {
		return "", fmt.Errorf("rag: encode vector: %w", err)
	}
	return string(b), nil
}

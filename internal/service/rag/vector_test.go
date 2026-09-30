package rag_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func ptr(f float64) *float64 { return &f }

func TestBuild_StandardizesPopulatedFieldsAndZeroesMissingOnes(t *testing.T) {
	v := rag.Build(rag.FeatureInput{
		Return1m:       ptr(0.01),
		Return5m:       ptr(0.02),
		PriceVsVWAPBps: ptr(50),
		SpreadBps:      ptr(20),
		// Every other field (including the four Feature Engine does not
		// compute yet: VolumeRatio1m, RealizedVol15m,
		// VolatilityExpansionRatio, StockVsSectorRelativeStrength) is left
		// nil.
	})

	want := rag.Vector{0: 1, 1: 1, 3: 1, 6: 1}
	if v != want {
		t.Errorf("Build() = %+v, want %+v (return_1m/return_5m/price_vs_vwap_bps/spread_bps = 1, everything else 0)", v, want)
	}
}

func TestBuild_AllNilInputYieldsZeroVector(t *testing.T) {
	v := rag.Build(rag.FeatureInput{})
	if v != (rag.Vector{}) {
		t.Errorf("Build(FeatureInput{}) = %+v, want the zero Vector (cold-start/no-data case)", v)
	}
}

func TestFeatureInputFromFeature_CarriesSpreadBpsFromSnapshotNotFeature(t *testing.T) {
	in := rag.FeatureInputFromFeature(domain.Feature{
		Return1m:       ptr(0.005),
		PriceVsVWAPBps: 25,
	}, ptr(10))

	if in.SpreadBps == nil || *in.SpreadBps != 10 {
		t.Errorf("FeatureInputFromFeature(...).SpreadBps = %v, want 10 (from the snapshot's spreadBps argument)", in.SpreadBps)
	}
	if in.Return1m == nil || *in.Return1m != 0.005 {
		t.Errorf("FeatureInputFromFeature(...).Return1m = %v, want 0.005", in.Return1m)
	}
	if in.PriceVsVWAPBps == nil || *in.PriceVsVWAPBps != 25 {
		t.Errorf("FeatureInputFromFeature(...).PriceVsVWAPBps = %v, want 25", in.PriceVsVWAPBps)
	}
}

func TestFeatureInputFromFeature_CarriesFormerlyMissingDimensions(t *testing.T) {
	f := domain.Feature{
		VolumeRatio1m: ptr(1.5), RealizedVol15m: ptr(0.02), VolatilityExpansionRatio: ptr(1.2),
		MarketReturn5m: ptr(0.001), SectorReturn5m: ptr(0.002), StockVsSectorRelativeStrength: ptr(0.03),
	}
	in := rag.FeatureInputFromFeature(f, nil)
	for name, got := range map[string]*float64{
		"VolumeRatio1m": in.VolumeRatio1m, "RealizedVol15m": in.RealizedVol15m,
		"VolatilityExpansionRatio": in.VolatilityExpansionRatio, "MarketReturn5m": in.MarketReturn5m,
		"SectorReturn5m": in.SectorReturn5m, "StockVsSectorRelativeStrength": in.StockVsSectorRelativeStrength,
	} {
		if got == nil {
			t.Errorf("%s = nil, want the Feature value carried into the RAG input", name)
		}
	}
}

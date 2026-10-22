package metalabel_test

import (
	"errors"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

func ptr(v float64) *float64 { return &v }

func fullFeatures() metalabel.Features {
	return metalabel.Features{
		OFIShort: ptr(1), OFIMid: ptr(2), OFILong: ptr(3), MicropriceDevBps: ptr(4),
		SpreadBps: ptr(5), Return1m: ptr(6), JevScore: ptr(0.7), TDnetScore: ptr(0.1),
	}
}

func TestFeatures_VectorOrientsDirectionalFeaturesTowardsTheCandidate(t *testing.T) {
	f := fullFeatures()
	long, err := f.Vector(domain.JevDirectionLong)
	if err != nil {
		t.Fatalf("LONG: %v", err)
	}
	want := []float64{1, 2, 3, 4, 5, 6, 0.7, 0.1}
	for i := range want {
		if long[i] != want[i] {
			t.Errorf("LONG[%d] = %v, want %v", i, long[i], want[i])
		}
	}
	short, err := f.Vector(domain.JevDirectionShort)
	if err != nil {
		t.Fatalf("SHORT: %v", err)
	}
	// OFI x3, microprice deviation and 1m return flip; spread and the scores do not.
	wantShort := []float64{-1, -2, -3, -4, 5, -6, 0.7, 0.1}
	for i := range wantShort {
		if short[i] != wantShort[i] {
			t.Errorf("SHORT[%d] = %v, want %v", i, short[i], wantShort[i])
		}
	}
	if got := len(metalabel.FeatureNames()); got != len(want) {
		t.Errorf("len(FeatureNames) = %d, want %d", got, len(want))
	}
}

func TestFeatures_VectorRejectsMissingAndNonFiniteAndNoCandidate(t *testing.T) {
	setters := map[string]func(*metalabel.Features, *float64){
		"ofi_short":          func(f *metalabel.Features, v *float64) { f.OFIShort = v },
		"ofi_mid":            func(f *metalabel.Features, v *float64) { f.OFIMid = v },
		"ofi_long":           func(f *metalabel.Features, v *float64) { f.OFILong = v },
		"microprice_dev_bps": func(f *metalabel.Features, v *float64) { f.MicropriceDevBps = v },
		"spread_bps":         func(f *metalabel.Features, v *float64) { f.SpreadBps = v },
		"return_1m":          func(f *metalabel.Features, v *float64) { f.Return1m = v },
		"jev_score":          func(f *metalabel.Features, v *float64) { f.JevScore = v },
		"tdnet_score":        func(f *metalabel.Features, v *float64) { f.TDnetScore = v },
	}
	for name, set := range setters {
		for label, v := range map[string]*float64{"nil": nil, "NaN": ptr(math.NaN()), "Inf": ptr(math.Inf(1))} {
			f := fullFeatures()
			set(&f, v)
			if _, err := f.Vector(domain.JevDirectionLong); !errors.Is(err, metalabel.ErrMissingFeature) {
				t.Errorf("%s=%s: error = %v, want ErrMissingFeature", name, label, err)
			}
		}
	}
	for _, dir := range []string{domain.JevDirectionNone, ""} {
		if _, err := fullFeatures().Vector(dir); !errors.Is(err, metalabel.ErrNoCandidate) {
			t.Errorf("direction %q: error = %v, want ErrNoCandidate", dir, err)
		}
	}
}

func TestNewFeatures_TakesJevScoreValue(t *testing.T) {
	flow := domain.OrderFlowFeatures{OFIShort: ptr(1), OFIMid: ptr(2), OFILong: ptr(3), MicropriceDevBps: ptr(4)}
	f := metalabel.NewFeatures(flow, ptr(5), ptr(6), ptr(1), ptr(0))
	if f.JevScore == nil || *f.JevScore != 1 || *f.OFIMid != 2 || *f.MicropriceDevBps != 4 || *f.SpreadBps != 5 {
		t.Fatalf("NewFeatures = %+v", f)
	}
	if g := metalabel.NewFeatures(flow, ptr(5), ptr(6), nil, ptr(0)); g.JevScore != nil {
		t.Error("no Jev score must stay nil, not 0")
	}
}

// MissingNames lists exactly the features Vector refuses, all of them (Vector
// reports the first), in FeatureNames order.
func TestFeatures_MissingNamesMatchesVector(t *testing.T) {
	if got := fullFeatures().MissingNames(); len(got) != 0 {
		t.Fatalf("MissingNames of a complete set = %v", got)
	}
	f := fullFeatures()
	f.OFIMid, f.SpreadBps, f.TDnetScore = nil, ptr(math.NaN()), ptr(math.Inf(-1))
	want := []string{metalabel.FeatureOFIMid, metalabel.FeatureSpreadBps, metalabel.FeatureTDnetScore}
	got := f.MissingNames()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("MissingNames = %v, want %v", got, want)
	}
	if _, err := f.Vector(domain.JevDirectionLong); !errors.Is(err, metalabel.ErrMissingFeature) {
		t.Errorf("Vector error = %v, want ErrMissingFeature for the same set", err)
	}
	if len(metalabel.Features{}.MissingNames()) != len(metalabel.FeatureNames()) {
		t.Errorf("an empty Features misses every feature")
	}
}

package slicetests

import (
	"math"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/slicediag"
)

// A bucket with fewer than MinBucketRows rows or MinBucketDays days is flagged low_sample with the reason(s), and exactly at the
// limits (30 rows, 5 days) it is not; the flag never removes the bucket from its family.
func TestLowSampleFlagAtTheEdges(t *testing.T) {
	srv, w := newServer(t)
	specs, sectors := lowSampleSpecs(t)
	seed(t, w, sectors, specs)
	d := srv.call(t, nil).data(t)
	if d.Statistics.MinBucketRows != 30 || d.Statistics.MinBucketDays != 5 || slicediag.MinBucketRows != 30 || slicediag.MinBucketDays != 5 {
		t.Fatalf("limits = %+v", d.Statistics)
	}
	for _, tc := range []struct {
		model   string
		n, days int
		reasons []string
	}{
		{"edge-ok", 30, 5, []string{}},
		{"edge-rows", 29, 5, []string{slicediag.ReasonRowsBelowMin}},
		{"edge-days", 30, 4, []string{slicediag.ReasonDaysBelowMin}},
	} {
		c := cohortOf(t, d, tc.model, v3)
		if c.All.N != tc.n || c.All.Days != tc.days || c.All.LowSample != (len(tc.reasons) > 0) || !slices.Equal(c.All.LowSampleReasons, tc.reasons) {
			t.Errorf("%s all = n %d days %d low %v %v, want %d/%d %v", tc.model, c.All.N, c.All.Days, c.All.LowSample, c.All.LowSampleReasons, tc.n, tc.days, tc.reasons)
		}
		// Every smaller bucket of the same cohort is flagged, and the flagged ones keep their p and Holm values.
		_, dir := dimOf(t, c, slicediag.DimDirection)
		for _, b := range dir.Buckets {
			small := b.N < 30 || b.Days < 5
			if b.LowSample != small || (small && len(b.LowSampleReasons) == 0) || (!small && len(b.LowSampleReasons) != 0) {
				t.Errorf("%s direction/%s: n %d days %d low_sample %v %v", tc.model, b.Bucket, b.N, b.Days, b.LowSample, b.LowSampleReasons)
			}
			if b.PValue != nil && b.HolmAdjustedP == nil {
				t.Errorf("%s direction/%s: a low-sample bucket with a p-value has no Holm value", tc.model, b.Bucket)
			}
		}
	}
}

// Holm is applied separately in each family (preregistered, exploratory) of a cohort across ALL the family's buckets that have a p-value
// (low-sample ones included): every adjusted p equals stats.Holm of the family's raw p-values, is >= its raw p, family_size counts exactly those buckets, and
// the two families have their own sizes. The all reference is in no family. Asking for fewer dimensions
// changes neither the family size nor the adjusted values.
func TestHolmWithinEachFamily(t *testing.T) {
	srv, w := newServer(t)
	specs, sectors := lowSampleSpecs(t)
	seed(t, w, sectors, specs)
	only := map[string]any{"model_id": "edge-ok"} // one cohort: the bootstrap of every bucket is what costs under -race
	d := srv.call(t, only).data(t)
	c := cohortOf(t, d, "edge-ok", v3)
	if len(c.Families) != 2 || c.Families[0].Family != slicediag.FamilyPreregistered || c.Families[1].Family != slicediag.FamilyExploratory {
		t.Fatalf("families = %+v", c.Families)
	}
	if c.All.HolmAdjustedP != nil {
		t.Errorf("all has a Holm value: %v", *c.All.HolmAdjustedP)
	}
	sizes := map[string]int{}
	moved := false
	for _, fam := range c.Families {
		var raw []float64
		var buckets []slicediag.Bucket
		total := 0
		for _, dim := range fam.Dimensions {
			for _, b := range dim.Buckets {
				total++
				if b.PValue != nil {
					raw = append(raw, *b.PValue)
					buckets = append(buckets, b)
				} else if b.HolmAdjustedP != nil {
					t.Errorf("%s/%s: a Holm value without a p-value", dim.Dimension, b.Bucket)
				}
			}
		}
		if len(raw) < 5 {
			t.Fatalf("%s: only %d buckets with a p-value, the test is vacuous", fam.Family, len(raw))
		}
		if fam.FamilySize != len(raw) || fam.Buckets != total {
			t.Errorf("%s: family_size %d buckets %d, want %d/%d", fam.Family, fam.FamilySize, fam.Buckets, len(raw), total)
		}
		sizes[fam.Family] = fam.FamilySize
		for i, adj := range stats.Holm(raw) {
			b := buckets[i]
			if b.HolmAdjustedP == nil || math.Abs(*b.HolmAdjustedP-adj) > eps || *b.HolmAdjustedP < raw[i]-eps || *b.HolmAdjustedP > 1 {
				t.Errorf("%s/%s: holm %v, want %v (raw %v)", fam.Family, b.Bucket, deref(b.HolmAdjustedP), adj, raw[i])
			}
			moved = moved || adj > raw[i]+1e-6
		}
	}
	if !moved {
		t.Error("no adjusted p differs from its raw p: the correction is not applied")
	}
	if sizes[slicediag.FamilyPreregistered] == sizes[slicediag.FamilyExploratory] {
		t.Errorf("the families have the same size %v: they may be mixed", sizes)
	}

	// Fewer dimensions: the same family size and the same adjusted values.
	part := srv.call(t, map[string]any{"model_id": "edge-ok", "dimensions": []string{"direction", "weekday"}}).data(t)
	if !slices.Equal(part.Dimensions, []string{slicediag.DimDirection, slicediag.DimWeekday}) {
		t.Errorf("dimensions = %v", part.Dimensions)
	}
	pc := cohortOf(t, part, "edge-ok", v3)
	if len(pc.Families) != 2 || len(pc.Families[0].Dimensions) != 1 || len(pc.Families[1].Dimensions) != 1 {
		t.Fatalf("filtered families = %+v", pc.Families)
	}
	for i, fam := range pc.Families {
		if fam.FamilySize != sizes[fam.Family] {
			t.Errorf("%s: family_size %d with dimensions, %d without", fam.Family, fam.FamilySize, sizes[fam.Family])
		}
		_, full := dimOf(t, c, fam.Dimensions[0].Dimension)
		for j, b := range fam.Dimensions[0].Buckets {
			if g, w := deref(b.HolmAdjustedP), deref(full.Buckets[j].HolmAdjustedP); g != w {
				t.Errorf("family %d %s/%s: holm %v with dimensions, %v without", i, fam.Dimensions[0].Dimension, b.Bucket, g, w)
			}
		}
	}
	// Only preregistered dimensions: the exploratory family is not returned.
	pre := srv.call(t, map[string]any{"model_id": "edge-ok", "dimensions": []string{"regime", "time_of_day"}}).data(t)
	if oc := cohortOf(t, pre, "edge-ok", v3); len(oc.Families) != 1 || oc.Families[0].Family != slicediag.FamilyPreregistered || oc.Families[0].FamilySize != sizes[slicediag.FamilyPreregistered] {
		t.Errorf("preregistered only = %+v", oc.Families)
	}
}

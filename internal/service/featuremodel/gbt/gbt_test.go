package gbt_test

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/gbt"
)

func params() gbt.Params {
	return gbt.Params{Trees: 40, Depth: 3, Rate: 0.2, MinLeaf: 20, Bins: 32}
}

// planted makes n rows of 3 features, uniform in [-1, 1): y is a step on x0 plus a
// slope on x1 and does not depend on x2.
func planted(n int, seed uint64) (x [][]float64, y []float64) {
	rng := rand.New(rand.NewPCG(seed, 1)) //nolint:gosec // G404: reproducible synthetic test data
	for range n {
		r := []float64{rng.Float64()*2 - 1, rng.Float64()*2 - 1, rng.Float64()*2 - 1}
		step := -5.0
		if r[0] > 0.2 {
			step = 5
		}
		x, y = append(x, r), append(y, step+3*r[1])
	}
	return x, y
}

// The trees learn a step and a slope a linear model cannot represent together, and
// ignore the feature that carries nothing.
func TestFitLearnsAStepAndASlope(t *testing.T) {
	x, y := planted(2000, 1)
	m, err := gbt.Fit(x, y, params())
	if err != nil {
		t.Fatal(err)
	}
	testX, testY := planted(500, 2)
	var sse, sst float64
	for i, r := range testX {
		d := m.Predict(r) - testY[i]
		sse += d * d
		sst += testY[i] * testY[i]
	}
	if sse/sst > 0.05 {
		t.Errorf("out-of-sample squared error is %.3f of the variance, want under 0.05", sse/sst)
	}
	// x2 carries nothing: moving it alone barely moves the prediction.
	for _, r := range testX[:50] {
		moved := []float64{r[0], r[1], -r[2]}
		if d := math.Abs(m.Predict(r) - m.Predict(moved)); d > 2 {
			t.Fatalf("prediction moved by %v with only the noise feature changed", d)
		}
	}
	if len(m.Trees) != 40 || m.Rate != 0.2 {
		t.Errorf("model has %d trees and rate %v, want 40 and 0.2", len(m.Trees), m.Rate)
	}
}

// The same rows and settings give the same model, to the last bit (compared through
// the JSON the run stores, and through the predictions).
func TestFitIsDeterministic(t *testing.T) {
	x, y := planted(1500, 3)
	a, err := gbt.Fit(x, y, params())
	if err != nil {
		t.Fatal(err)
	}
	b, err := gbt.Fit(x, y, params())
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("two fits of the same rows differ")
	}
	var back gbt.Model
	if err := json.Unmarshal(ja, &back); err != nil {
		t.Fatal(err)
	}
	for _, r := range x[:100] {
		if a.Predict(r) != back.Predict(r) {
			t.Fatal("the stored JSON does not reproduce the prediction")
		}
	}
}

// A tie goes to the lowest feature: two identical columns split on the first, and the
// second is never used.
func TestTiesGoToTheLowestFeature(t *testing.T) {
	x, y := planted(800, 4)
	for _, r := range x {
		r[2] = r[0]
	}
	m, err := gbt.Fit(x, y, params())
	if err != nil {
		t.Fatal(err)
	}
	for _, tree := range m.Trees {
		for _, n := range tree {
			if n.Feature == 2 {
				t.Fatal("a split used the duplicate column instead of the lowest feature")
			}
		}
	}
}

// MinLeaf and Depth hold in every tree; a constant target gives a constant model.
func TestLeavesAndDepthRespectTheLimits(t *testing.T) {
	x, y := planted(1000, 5)
	p := params()
	p.Depth, p.MinLeaf = 2, 150
	m, err := gbt.Fit(x, y, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, tree := range m.Trees {
		if len(tree) > 7 { // 3 splits and 4 leaves at depth 2
			t.Fatalf("tree of %d nodes is deeper than 2 splits", len(tree))
		}
	}
	counts := map[*gbt.Node]int{}
	for _, r := range x {
		i := 0
		tree := m.Trees[0]
		for tree[i].Feature >= 0 {
			if r[tree[i].Feature] <= tree[i].Threshold {
				i = tree[i].Left
			} else {
				i = tree[i].Right
			}
		}
		counts[&tree[i]]++
	}
	for _, c := range counts {
		if c < 150 {
			t.Fatalf("a leaf holds %d rows, want at least 150", c)
		}
	}
	flat := make([]float64, len(y))
	for i := range flat {
		flat[i] = 7
	}
	c, err := gbt.Fit(x, flat, params())
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Predict(x[0]); math.Abs(got-7) > 1e-9 {
		t.Errorf("constant target predicted %v, want 7", got)
	}
}

func TestFitRejectsBadInput(t *testing.T) {
	x, y := planted(100, 6)
	bad := x[0]
	for name, c := range map[string]struct {
		x [][]float64
		y []float64
		p gbt.Params
	}{
		"zero trees":      {x, y, gbt.Params{Trees: 0, Depth: 3, Rate: 0.1, MinLeaf: 10, Bins: 8}},
		"rate above 1":    {x, y, gbt.Params{Trees: 5, Depth: 3, Rate: 1.5, MinLeaf: 10, Bins: 8}},
		"too many bins":   {x, y, gbt.Params{Trees: 5, Depth: 3, Rate: 0.1, MinLeaf: 10, Bins: 300}},
		"target mismatch": {x, y[:50], params()},
		"too few rows":    {x[:30], y[:30], params()},
		"NaN value":       {append([][]float64{{math.NaN(), bad[1], bad[2]}}, x[1:]...), y, params()},
		"ragged row":      {append([][]float64{bad[:2]}, x[1:]...), y, params()},
	} {
		if _, err := gbt.Fit(c.x, c.y, c.p); err == nil {
			t.Errorf("%s: Fit succeeded, want an error", name)
		}
	}
}

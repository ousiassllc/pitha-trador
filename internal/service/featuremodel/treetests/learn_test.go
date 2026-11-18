package treetests

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// Each model learns the horizon it is trained on, from that horizon's own labels: the
// fixture's 60m labels are twice the 30m ones and the close labels have the opposite
// sign, so a model fed another horizon's labels predicts the wrong size or sign. Both
// algorithms are checked at +8 and -8 in return_5m (long ~ +3x, short ~ -2x, times the
// horizon's scale).
func TestEachModelLearnsItsOwnHorizon(t *testing.T) {
	db, _, d := history(t)
	for _, spec := range featuremodel.Specs() {
		tr := mustTrain(t, db, spec, d)
		scale := modeltest.HorizonScale(spec.Horizon)
		if tr.Horizon != spec.Horizon || tr.NLong != 600 || tr.NShort != 600 || tr.NTrain != 600 || tr.Days != 6 || tr.From != "2026-10-05" || tr.To != "2026-10-10" {
			t.Errorf("%s/%s window = %+v, want %s labels, 6 days 2026-10-05..10 and 600 rows", spec.Algorithm, spec.Horizon, tr, spec.Horizon)
		}
		p := tr.Predictor()
		longHi, shortHi := p.Predict(atReturn5m(8))
		longLo, shortLo := p.Predict(atReturn5m(-8))
		wantLong, wantShort := 3*8*scale, -2*8*scale
		for _, c := range []struct {
			name      string
			got, want float64
		}{{"long at +8", longHi, wantLong}, {"long at -8", longLo, -wantLong}, {"short at +8", shortHi, wantShort}, {"short at -8", shortLo, -wantShort}} {
			if r := c.got / c.want; r < 0.6 || r > 1.4 {
				t.Errorf("%s/%s %s = %.2f, want about %.2f (ratio %.2f outside 0.6..1.4)", spec.Algorithm, spec.Horizon, c.name, c.got, c.want, r)
			}
		}
		if spec.Algorithm == featuremodel.AlgorithmTree && (tr.Tree == nil || tr.Tree.NLong != 600 || len(tr.Tree.Long.Trees) != testTreeParams().Trees) {
			t.Errorf("%s tree model = %+v", spec.Horizon, tr.Tree)
		}
	}
}

// Same data and settings give identical models, in the same database and in a second
// one built the same way: the digest, the stored JSON and every prediction match to
// the last bit (exact equality, no tolerance). The same holds on this platform for any
// run; across CPU architectures the trees avoid fused multiply-add by construction
// (float64(...) around every product) but that is not verified here.
func TestTrainingIsReproducible(t *testing.T) {
	db, _, d := history(t)
	db2, _, _ := history(t)
	for _, spec := range featuremodel.Specs() {
		a, b, c := mustTrain(t, db, spec, d), mustTrain(t, db, spec, d), mustTrain(t, db2, spec, d)
		ja, _ := json.Marshal(modelOf(a))
		for i, o := range []*featuremodel.Trained{b, c} {
			jo, _ := json.Marshal(modelOf(o))
			if o.Digest != a.Digest || len(a.Digest) != 64 || string(jo) != string(ja) {
				t.Errorf("%s/%s repeat %d differs: digest %q vs %q, models equal %v", spec.Algorithm, spec.Horizon, i, o.Digest, a.Digest, string(jo) == string(ja))
			}
			for _, v := range []float64{-9, -3, 0, 4, 8.5} {
				l1, s1 := a.Predictor().Predict(atReturn5m(v))
				l2, s2 := o.Predictor().Predict(atReturn5m(v))
				if l1 != l2 || s1 != s2 {
					t.Errorf("%s/%s repeat %d predicts (%v, %v) vs (%v, %v) at %v", spec.Algorithm, spec.Horizon, i, l2, s2, l1, s1, v)
				}
			}
		}
	}
}

// modelOf is the stored form of the fitted model.
func modelOf(t *featuremodel.Trained) any {
	if t.Tree != nil {
		return t.Tree
	}
	return t.Model
}

// The digests of the models differ from each other (algorithm and horizon are in the
// digest), and change with the rows.
func TestDigestsAreSeparatePerModelAndRows(t *testing.T) {
	db, _, d := history(t)
	seen := map[string]string{}
	for _, spec := range featuremodel.Specs() {
		dg := mustTrain(t, db, spec, d).Digest
		if other, ok := seen[dg]; ok {
			t.Errorf("%s/%s and %s share the digest %s", spec.Algorithm, spec.Horizon, other, dg)
		}
		seen[dg] = spec.Algorithm + "/" + spec.Horizon
	}
	modeltest.SeedDay(t, db, "2026-10-04", modeltest.Day{Rows: 10, Seed: 99, Mark: true})
	modeltest.SeedHorizons(t, db, "2026-10-04")
	for _, spec := range featuremodel.Specs() {
		for dg, name := range seen {
			if name == spec.Algorithm+"/"+spec.Horizon && mustTrain(t, db, spec, d).Digest == dg {
				t.Errorf("%s: the digest did not change with the training rows", name)
			}
		}
	}
}

func TestDefaultTreeParams(t *testing.T) {
	want := featuremodel.TreeParams{Trees: 60, Depth: 3, Rate: 0.1, MinLeaf: 100, Bins: 32, MaxRows: 200000}
	if got := featuremodel.DefaultTreeParams(); got != want {
		t.Errorf("DefaultTreeParams = %+v, want %+v", got, want)
	}
}

// Version names: the baseline keeps rm-ridge1-30m-, the new models are rm-<algorithm>1-<horizon>-.
func TestSpecsAndVersionNames(t *testing.T) {
	var got []string
	for _, s := range featuremodel.Specs() {
		got = append(got, s.Version("2026-10-11"))
	}
	want := []string{
		"rm-ridge1-30m-2026-10-11", "rm-gbt1-30m-2026-10-11", "rm-ridge1-60m-2026-10-11", "rm-gbt1-60m-2026-10-11", "rm-ridge1-close-2026-10-11", "rm-gbt1-close-2026-10-11",
	}
	if len(got) != len(want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("version %d = %s, want %s", i, got[i], want[i])
		}
	}
	if featuremodel.Version("2026-10-11") != want[0] || featuremodel.Baseline.Version("2026-10-11") != want[0] {
		t.Error("the baseline's version changed")
	}
}
